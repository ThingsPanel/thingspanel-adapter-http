package downlink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
	PublishCommandResponse(deviceID, messageID string, ok bool, data interface{}) error
}

type Processor struct {
	platform   PlatformClient
	httpClient *http.Client
	logger     *logrus.Logger
	mu         sync.Mutex
	queues     map[string][]QueuedMessage
	inflight   map[string]QueuedMessage
	notify     map[string]chan struct{}
}

type Config struct {
	CommandURL string
	ControlURL string
	Port       string
}

type QueuedMessage struct {
	Type         string      `json:"type"`
	DeviceID     string      `json:"device_id,omitempty"`
	DeviceNumber string      `json:"device_number"`
	MessageID    string      `json:"message_id,omitempty"`
	Method       string      `json:"method,omitempty"`
	Params       interface{} `json:"params,omitempty"`
	Values       interface{} `json:"values,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

func NewProcessor(platform PlatformClient, logger *logrus.Logger) *Processor {
	return &Processor{
		platform: platform,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:   logger,
		queues:   make(map[string][]QueuedMessage),
		inflight: make(map[string]QueuedMessage),
		notify:   make(map[string]chan struct{}),
	}
}

func (p *Processor) ProcessCommand(deviceID, messageID string, message platform.CommandMessage) error {
	device, err := p.platform.GetDeviceByID(deviceID)
	if err != nil {
		return fmt.Errorf("query device failed: device_id=%s: %w", deviceID, err)
	}

	queueItem := QueuedMessage{
		Type:         "command",
		DeviceID:     deviceID,
		DeviceNumber: device.DeviceNumber,
		MessageID:    messageID,
		Method:       message.Method,
		Params:       message.Params,
		CreatedAt:    time.Now(),
	}

	if directHost(device) == "" {
		p.enqueue(queueItem)
		return nil
	}

	cfg := parseConfig(device.Config)
	body := map[string]interface{}{
		"tp_message_id":    messageID,
		"tp_device_id":     deviceID,
		"tp_device_number": device.DeviceNumber,
		"method":           message.Method,
		"params":           message.Params,
	}

	respBody, err := p.post(device, cfg.CommandURL, cfg, body)
	if err != nil {
		p.logger.WithError(err).Warnf("HTTP direct command failed, queued for long polling: device_id=%s, device_number=%s", deviceID, device.DeviceNumber)
		p.enqueue(queueItem)
		return nil
	}

	var deviceResp struct {
		MessageID string      `json:"message_id"`
		OK        bool        `json:"ok"`
		Data      interface{} `json:"data"`
	}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &deviceResp); err != nil {
			p.logger.WithError(err).Warn("decode device command response failed")
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
		}).Warn("device command response message_id mismatch")
		return nil
	}

	if err := p.platform.PublishCommandResponse(deviceID, messageID, deviceResp.OK, deviceResp.Data); err != nil {
		p.logger.WithError(err).Error("publish command response failed")
	}
	return nil
}

func (p *Processor) ProcessControl(deviceNumber string, controlData map[string]interface{}) error {
	device, err := p.platform.GetDevice(deviceNumber)
	if err != nil {
		return fmt.Errorf("query device failed: device_number=%s: %w", deviceNumber, err)
	}

	queueItem := QueuedMessage{
		Type:         "control",
		DeviceID:     device.ID,
		DeviceNumber: deviceNumber,
		Values:       controlData,
		CreatedAt:    time.Now(),
	}

	if directHost(device) == "" {
		p.enqueue(queueItem)
		return nil
	}

	cfg := parseConfig(device.Config)
	body := map[string]interface{}{
		"tp_device_number": deviceNumber,
		"values":           controlData,
	}

	if _, err := p.post(device, cfg.ControlURL, cfg, body); err != nil {
		p.logger.WithError(err).Warnf("HTTP direct control failed, queued for long polling: device_number=%s", deviceNumber)
		p.enqueue(queueItem)
		return nil
	}

	p.logger.WithFields(logrus.Fields{
		"device_number": deviceNumber,
		"control_data":  controlData,
	}).Info("device control message sent")
	return nil
}

func (p *Processor) Poll(ctx context.Context, deviceNumber string, timeout time.Duration) ([]QueuedMessage, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		p.mu.Lock()
		if len(p.queues[deviceNumber]) > 0 {
			items := p.queues[deviceNumber]
			delete(p.queues, deviceNumber)
			for _, item := range items {
				if item.Type == "command" && item.MessageID != "" {
					p.inflight[inflightKey(deviceNumber, item.MessageID)] = item
				}
			}
			p.mu.Unlock()
			return items, nil
		}
		ch := p.notifyChanLocked(deviceNumber)
		p.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return []QueuedMessage{}, nil
		case <-ch:
		}
	}
}

func (p *Processor) AckCommand(deviceNumber, messageID string, ok bool, data interface{}) error {
	key := inflightKey(deviceNumber, messageID)

	p.mu.Lock()
	item, exists := p.inflight[key]
	if exists {
		delete(p.inflight, key)
	}
	p.mu.Unlock()

	if !exists {
		device, err := p.platform.GetDevice(deviceNumber)
		if err != nil {
			return fmt.Errorf("ack command not found and device lookup failed: device_number=%s, message_id=%s: %w", deviceNumber, messageID, err)
		}
		item = QueuedMessage{DeviceID: device.ID, DeviceNumber: deviceNumber, MessageID: messageID}
	}

	return p.platform.PublishCommandResponse(item.DeviceID, messageID, ok, data)
}

func (p *Processor) post(device *types.Device, path string, cfg Config, body map[string]interface{}) ([]byte, error) {
	host := directHost(device)
	if host == "" {
		return nil, fmt.Errorf("device direct downlink host is empty")
	}

	endpoint, err := buildURL(host, cfg.Port, path)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal downlink request failed: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create downlink request failed: %w", err)
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
	}).Info("sending HTTP direct downlink request")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP downlink request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("device response is not 2xx: status=%s, body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func (p *Processor) enqueue(item QueuedMessage) {
	p.mu.Lock()
	p.queues[item.DeviceNumber] = append(p.queues[item.DeviceNumber], item)
	ch := p.notifyChanLocked(item.DeviceNumber)
	delete(p.notify, item.DeviceNumber)
	close(ch)
	p.mu.Unlock()

	p.logger.WithFields(logrus.Fields{
		"device_number": item.DeviceNumber,
		"type":          item.Type,
		"message_id":    item.MessageID,
	}).Info("downlink message queued for long polling")
}

func (p *Processor) notifyChanLocked(deviceNumber string) chan struct{} {
	ch, ok := p.notify[deviceNumber]
	if !ok {
		ch = make(chan struct{})
		p.notify[deviceNumber] = ch
	}
	return ch
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
	if token := voucherString(device, "accessToken", "access_token", "token"); token != "" {
		return token
	}
	if device.Voucher != "" && !strings.HasPrefix(strings.TrimSpace(device.Voucher), "{") {
		return strings.TrimSpace(device.Voucher)
	}
	return ""
}

func directHost(device *types.Device) string {
	if host := voucherString(device, "downlinkHost", "downlink_host", "host", "deviceHost", "device_host", "ip", "address", "addr"); host != "" {
		return host
	}
	return firstString(device.Config, "downlinkHost", "downlink_host", "host", "deviceHost", "device_host", "ip", "address", "addr")
}

func voucherString(device *types.Device, keys ...string) string {
	if device.Voucher == "" {
		return ""
	}
	var voucher map[string]interface{}
	if err := json.Unmarshal([]byte(device.Voucher), &voucher); err != nil {
		return ""
	}
	return firstString(voucher, keys...)
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
			return "", fmt.Errorf("invalid device host: %w", err)
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

func inflightKey(deviceNumber, messageID string) string {
	return deviceNumber + "\x00" + messageID
}
