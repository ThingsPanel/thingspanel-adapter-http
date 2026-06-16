package downlink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"tp-plugin/internal/platform"

	"github.com/ThingsPanel/tp-protocol-sdk-go/types"
	"github.com/sirupsen/logrus"
)

const (
	defaultCommandURL = "/api/v1/command"
	defaultControlURL = "/api/v1/control"
	defaultDevicePort = "8080"
)

type PlatformClient interface {
	GetDevice(deviceNumber string) (*types.Device, error)
	GetDeviceByID(deviceID string) (*types.Device, error)
	GetDeviceAddress(deviceNumber string) (string, bool)
	PublishCommandResponse(deviceID, messageID string, ok bool, data interface{}) error
}

type Processor struct {
	platform   PlatformClient
	httpClient *http.Client
	logger     *logrus.Logger
}

type Config struct {
	CommandURL string
	ControlURL string
	Port       string
}

func NewProcessor(platform PlatformClient, logger *logrus.Logger) *Processor {
	return &Processor{
		platform: platform,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

func (p *Processor) ProcessCommand(deviceID, messageID string, message platform.CommandMessage) error {
	device, err := p.platform.GetDeviceByID(deviceID)
	if err != nil {
		return fmt.Errorf("查询设备失败: device_id=%s: %w", deviceID, err)
	}

	cfg := parseConfig(device.Config)
	body := map[string]interface{}{
		"tp_message_id": messageID,
		"tp_device_id":  deviceID,
		"method":        message.Method,
		"params":        message.Params,
	}

	respBody, err := p.post(device, cfg.CommandURL, cfg, body)
	if err != nil {
		p.logger.WithError(err).Errorf("下发 Command 失败: device_id=%s, device_number=%s", deviceID, device.DeviceNumber)
		return nil
	}

	var deviceResp struct {
		MessageID string      `json:"message_id"`
		OK        bool        `json:"ok"`
		Data      interface{} `json:"data"`
	}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &deviceResp); err != nil {
			p.logger.WithError(err).Warn("解析设备 Command 响应失败")
			return nil
		}
	}
	if deviceResp.MessageID == "" {
		deviceResp.MessageID = messageID
	}
	if deviceResp.MessageID != messageID {
		p.logger.WithFields(logrus.Fields{
			"device_id":       deviceID,
			"message_id":      messageID,
			"resp_message_id": deviceResp.MessageID,
		}).Warn("设备响应 message_id 不匹配")
		return nil
	}

	if err := p.platform.PublishCommandResponse(deviceID, messageID, deviceResp.OK, deviceResp.Data); err != nil {
		p.logger.WithError(err).Error("回传 Command 响应失败")
	}
	return nil
}

func (p *Processor) ProcessControl(deviceNumber string, controlData map[string]interface{}) error {
	device, err := p.platform.GetDevice(deviceNumber)
	if err != nil {
		return fmt.Errorf("查询设备失败: device_number=%s: %w", deviceNumber, err)
	}

	cfg := parseConfig(device.Config)
	body := map[string]interface{}{
		"tp_device_number": deviceNumber,
		"values":           controlData,
	}

	if _, err := p.post(device, cfg.ControlURL, cfg, body); err != nil {
		p.logger.WithError(err).Errorf("下发 Control 失败: device_number=%s", deviceNumber)
		return nil
	}

	p.logger.WithFields(logrus.Fields{
		"device_number": deviceNumber,
		"control_data":  controlData,
	}).Info("设备控制消息已下发")
	return nil
}

func (p *Processor) post(device *types.Device, path string, cfg Config, body map[string]interface{}) ([]byte, error) {
	host := ""
	if address, ok := p.platform.GetDeviceAddress(device.DeviceNumber); ok {
		host = address
	}
	if host == "" {
		host = firstString(device.Config, "host", "deviceHost", "device_host", "ip", "address", "addr")
	}
	if host == "" {
		return nil, fmt.Errorf("设备下行地址未知：设备尚未上报，且未在配置中设置 host/deviceHost/ip/address")
	}

	endpoint, err := buildURL(host, cfg.Port, path)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化下发数据失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建下发请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := accessToken(device); token != "" {
		req.Header.Set("X-Api-Key", token)
		req.Header.Set("Access-Token", token)
	}

	p.logger.WithFields(logrus.Fields{
		"device_number": device.DeviceNumber,
		"url":           endpoint,
		"body":          string(bodyBytes),
	}).Info("准备下发 HTTP 控制请求")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP 下发请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("设备响应非 2xx: status=%s, body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func parseConfig(config map[string]interface{}) Config {
	return Config{
		CommandURL: withDefault(firstString(config, "commandUrl", "command_url"), defaultCommandURL),
		ControlURL: withDefault(firstString(config, "controlUrl", "control_url"), defaultControlURL),
		Port:       withDefault(firstString(config, "port", "devicePort", "device_port"), defaultDevicePort),
	}
}

func accessToken(device *types.Device) string {
	if token := firstString(device.Config, "accessToken", "access_token", "token"); token != "" {
		return token
	}
	if device.Voucher == "" {
		return ""
	}
	var voucher map[string]interface{}
	if err := json.Unmarshal([]byte(device.Voucher), &voucher); err != nil {
		return device.Voucher
	}
	return firstString(voucher, "accessToken", "access_token", "token")
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		raw, ok := m[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case json.Number:
			return v.String()
		case float64:
			if v == float64(int64(v)) {
				return strconv.FormatInt(int64(v), 10)
			}
			return strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			return strconv.Itoa(v)
		case int64:
			return strconv.FormatInt(v, 10)
		}
	}
	return ""
}

func withDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func buildURL(host, port, path string) (string, error) {
	host = strings.TrimSpace(host)
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		u, err := url.Parse(host)
		if err != nil {
			return "", fmt.Errorf("设备地址格式错误: %w", err)
		}
		if u.Port() == "" && port != "" {
			u.Host = net.JoinHostPort(u.Hostname(), port)
		}
		u.Path = path
		u.RawQuery = ""
		return u.String(), nil
	}

	return "http://" + net.JoinHostPort(host, port) + path, nil
}
