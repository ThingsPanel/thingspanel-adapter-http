package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ThingsPanel/tp-protocol-sdk-go/client"
	"github.com/ThingsPanel/tp-protocol-sdk-go/types"
	"github.com/sirupsen/logrus"
)

const (
	DeviceStatusOffline = 0
	DeviceStatusOnline  = 1
)

type CommandProcessorInterface interface {
	ProcessCommand(deviceID, messageID string, message CommandMessage) error
}

type ControlProcessorInterface interface {
	ProcessControl(deviceNumber string, controlData map[string]interface{}) error
}

type CommandMessage struct {
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

type PlatformClient struct {
	sdkClient        *client.Client
	logger           *logrus.Logger
	deviceCache      map[string]*types.Device
	deviceIDCache    map[string]*types.Device
	deviceAddrCache  map[string]string
	cacheMutex       sync.RWMutex
	Config           Config
	commandProcessor CommandProcessorInterface
	controlProcessor ControlProcessorInterface
}

type Config struct {
	BaseURL               string
	MQTTBroker            string
	MQTTUsername          string
	MQTTPassword          string
	ServiceIdentifier     string
	TemplateSecret        string
	SubTemplateSecret     string
	GatewayTemplateSecret string
}

func NewPlatformClient(config Config, logger *logrus.Logger) (*PlatformClient, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	sdkConfig := client.ClientConfig{
		BaseURL:      baseURL,
		MQTTBroker:   config.MQTTBroker,
		MQTTUsername: config.MQTTUsername,
		MQTTPassword: config.MQTTPassword,
		MQTTClientID: fmt.Sprintf("%s-%d", config.ServiceIdentifier, time.Now().Unix()),
	}
	logrus.Infof("sdkConfig: %+v", sdkConfig)

	sdkClient, err := client.NewClient(sdkConfig)
	if err != nil {
		return nil, err
	}
	if err := sdkClient.Connect(); err != nil {
		return nil, err
	}

	return &PlatformClient{
		Config:          config,
		sdkClient:       sdkClient,
		logger:          logger,
		deviceCache:     make(map[string]*types.Device),
		deviceIDCache:   make(map[string]*types.Device),
		deviceAddrCache: make(map[string]string),
	}, nil
}

func (p *PlatformClient) GetDevice(deviceNumber string) (*types.Device, error) {
	p.cacheMutex.RLock()
	if device, ok := p.deviceCache[deviceNumber]; ok && device.ID != "" {
		p.cacheMutex.RUnlock()
		return device, nil
	}
	p.cacheMutex.RUnlock()

	req := &client.DeviceConfigRequest{DeviceNumber: deviceNumber}
	resp, err := p.sdkClient.Device().GetDeviceConfig(context.Background(), req)
	if err != nil {
		return nil, err
	}

	p.logger.WithFields(logrus.Fields{
		"device_number": deviceNumber,
		"code":          resp.Code,
		"message":       resp.Message,
		"device_id":     resp.Data.ID,
	}).Debug("get device config response")
	if resp.Code != 200 {
		return nil, fmt.Errorf("获取设备配置失败: device_number=%s, code=%d, message=%s", deviceNumber, resp.Code, resp.Message)
	}
	if resp.Data.ID == "" {
		return nil, fmt.Errorf("设备不存在: device_number=%s", deviceNumber)
	}

	p.cacheMutex.Lock()
	p.deviceCache[deviceNumber] = &resp.Data
	p.deviceIDCache[resp.Data.ID] = &resp.Data
	p.cacheMutex.Unlock()

	return &resp.Data, nil
}

func (p *PlatformClient) DynamicRegister(deviceNumber string) (*types.DeviceDynamicAuthData, error) {
	req := &client.DeviceDynamicAuthRequest{
		TemplateSecret: p.Config.TemplateSecret,
		DeviceNumber:   deviceNumber,
		DeviceName:     p.Config.ServiceIdentifier + "-" + deviceNumber,
	}

	resp, err := p.sdkClient.Device().DeviceDynamicAuth(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("直连设备动态注册失败: %s", resp.Message)
	}
	return &resp.Data, nil
}

func (p *PlatformClient) SubDeviceDynamicRegister(deviceNumber string, subDeviceAddr string, parentDeviceNumber string) (*types.DeviceDynamicAuthData, error) {
	if p.Config.SubTemplateSecret == "" {
		return nil, fmt.Errorf("子设备模板密钥未配置")
	}

	req := &client.DeviceDynamicAuthRequest{
		TemplateSecret:     p.Config.SubTemplateSecret,
		DeviceNumber:       deviceNumber,
		DeviceName:         p.Config.ServiceIdentifier + "-SUB-" + deviceNumber,
		SubDeviceAddr:      subDeviceAddr,
		ParentDeviceNumber: parentDeviceNumber,
	}

	resp, err := p.sdkClient.Device().DeviceDynamicAuth(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("子设备动态注册失败: %s", resp.Message)
	}
	return &resp.Data, nil
}

func (p *PlatformClient) GatewayDynamicRegister(deviceNumber string) (*types.DeviceDynamicAuthData, error) {
	if p.Config.GatewayTemplateSecret == "" {
		return nil, fmt.Errorf("网关模板密钥未配置")
	}

	req := &client.DeviceDynamicAuthRequest{
		TemplateSecret: p.Config.GatewayTemplateSecret,
		DeviceNumber:   deviceNumber,
		DeviceName:     p.Config.ServiceIdentifier + "-网关-" + deviceNumber,
	}

	resp, err := p.sdkClient.Device().DeviceDynamicAuth(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("网关动态注册失败: %s", resp.Message)
	}
	return &resp.Data, nil
}

func (p *PlatformClient) GetServiceAccessPoints() ([]types.ServiceAccessRsp, error) {
	req := &client.ServiceAccessRequest{
		ServiceIdentifier: p.Config.ServiceIdentifier,
	}
	resp, err := p.sdkClient.Service().GetServiceAccessList(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("获取服务接入点列表失败: code=%d, message=%s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

func (p *PlatformClient) ClearDeviceCache(deviceNumber string) {
	p.cacheMutex.Lock()
	if device, exists := p.deviceCache[deviceNumber]; exists {
		delete(p.deviceIDCache, device.ID)
	}
	delete(p.deviceCache, deviceNumber)
	delete(p.deviceAddrCache, deviceNumber)
	p.cacheMutex.Unlock()
	p.logger.WithField("device_number", deviceNumber).Debug("device cache cleared")
}

func (p *PlatformClient) UpdateDeviceAddress(deviceNumber, address string) {
	address = strings.TrimSpace(address)
	if deviceNumber == "" || address == "" {
		return
	}

	p.cacheMutex.Lock()
	p.deviceAddrCache[deviceNumber] = address
	p.cacheMutex.Unlock()

	p.logger.WithFields(logrus.Fields{
		"device_number": deviceNumber,
		"address":       address,
	}).Debug("device uplink address updated")
}

func (p *PlatformClient) GetDeviceAddress(deviceNumber string) (string, bool) {
	p.cacheMutex.RLock()
	address, ok := p.deviceAddrCache[deviceNumber]
	p.cacheMutex.RUnlock()
	return address, ok && address != ""
}

func (p *PlatformClient) GetDeviceByID(deviceID string) (*types.Device, error) {
	p.cacheMutex.RLock()
	if device, ok := p.deviceIDCache[deviceID]; ok && device.ID != "" {
		p.cacheMutex.RUnlock()
		return device, nil
	}
	p.cacheMutex.RUnlock()

	req := &client.DeviceConfigRequest{DeviceID: deviceID}
	resp, err := p.sdkClient.Device().GetDeviceConfig(context.Background(), req)
	if err != nil {
		return nil, err
	}
	p.logger.WithFields(logrus.Fields{
		"device_id":     deviceID,
		"code":          resp.Code,
		"message":       resp.Message,
		"device_number": resp.Data.DeviceNumber,
	}).Debug("get device config by id response")
	if resp.Code != 200 {
		return nil, fmt.Errorf("获取设备配置失败: device_id=%s, code=%d, message=%s", deviceID, resp.Code, resp.Message)
	}
	if resp.Data.ID == "" {
		return nil, fmt.Errorf("设备不存在: device_id=%s", deviceID)
	}

	p.cacheMutex.Lock()
	p.deviceCache[resp.Data.DeviceNumber] = &resp.Data
	p.deviceIDCache[resp.Data.ID] = &resp.Data
	p.cacheMutex.Unlock()

	return &resp.Data, nil
}

func (p *PlatformClient) SendTelemetry(deviceID string, values map[string]interface{}) error {
	valuesJSON, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("序列化 values 失败: %w", err)
	}

	msg := map[string]interface{}{
		"device_id": deviceID,
		"values":    base64.StdEncoding.EncodeToString(valuesJSON),
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化遥测消息失败: %w", err)
	}

	if err := p.sdkClient.MQTT().Publish("devices/telemetry", 1, string(payload)); err != nil {
		return fmt.Errorf("发送遥测消息失败: %w", err)
	}

	p.logger.WithField("device_id", deviceID).Debug("telemetry sent")
	return nil
}

func (p *PlatformClient) Close() {
	if p.sdkClient != nil {
		p.sdkClient.Close()
	}
}

func (p *PlatformClient) SendDeviceStatus(deviceID string, status int) error {
	if status != DeviceStatusOffline && status != DeviceStatusOnline {
		return fmt.Errorf("无效的设备状态值: %d", status)
	}

	payload := []byte(fmt.Sprintf("%d", status))
	if err := p.sdkClient.MQTT().Publish("devices/status/"+deviceID, 1, payload); err != nil {
		return fmt.Errorf("发送状态消息失败: %w", err)
	}

	p.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
		"status":    status,
	}).Debug("device status sent")
	return nil
}

func (p *PlatformClient) PublishCommandResponse(deviceID, messageID string, ok bool, data interface{}) error {
	payload, err := json.Marshal(map[string]interface{}{
		"ok":   ok,
		"data": data,
	})
	if err != nil {
		return fmt.Errorf("序列化指令响应失败: %w", err)
	}

	topic := fmt.Sprintf("plugin/%s/devices/command_response/%s/%s", p.Config.ServiceIdentifier, deviceID, messageID)
	if err := p.sdkClient.MQTT().Publish(topic, 1, string(payload)); err != nil {
		return fmt.Errorf("发布指令响应失败: %w", err)
	}

	p.logger.WithFields(logrus.Fields{
		"device_id":  deviceID,
		"message_id": messageID,
		"topic":      topic,
	}).Info("command response published")
	return nil
}

func (p *PlatformClient) SendHeartbeat(ctx context.Context, serviceIdentifier string) error {
	req := &client.HeartbeatRequest{
		ServiceIdentifier: serviceIdentifier,
	}

	resp, err := p.sdkClient.Service().SendHeartbeat(ctx, req)
	if err != nil {
		return fmt.Errorf("发送心跳失败: %w", err)
	}
	if resp.Code != 200 {
		return fmt.Errorf("心跳响应异常: code=%d, message=%s", resp.Code, resp.Message)
	}
	return nil
}

func (p *PlatformClient) SetCommandProcessor(processor CommandProcessorInterface) {
	p.commandProcessor = processor
	p.logger.Info("command processor configured")
	if err := p.startCommandSubscription(); err != nil {
		p.logger.WithError(err).Error("start command subscription failed")
	}
}

func (p *PlatformClient) SetControlProcessor(processor ControlProcessorInterface) {
	p.controlProcessor = processor
	p.logger.Info("control processor configured")
	if err := p.startControlSubscription(); err != nil {
		p.logger.WithError(err).Error("start control subscription failed")
	}
}

func (p *PlatformClient) startCommandSubscription() error {
	commandTopic := fmt.Sprintf("plugin/%s/devices/command/+/+", p.Config.ServiceIdentifier)
	p.logger.Infof("subscribing command topic: %s", commandTopic)

	if err := p.sdkClient.MQTT().Subscribe(commandTopic, 1, p.handleCommandMessage); err != nil {
		return fmt.Errorf("订阅指令主题失败: %w", err)
	}
	return nil
}

func (p *PlatformClient) handleCommandMessage(topic string, payload []byte) {
	p.logger.Debugf("command message received: topic=%s, payload=%s", topic, string(payload))

	parts := strings.Split(topic, "/")
	if len(parts) != 6 {
		p.logger.Errorf("invalid command topic: %s", topic)
		return
	}

	deviceID := parts[4]
	messageID := parts[5]

	var message CommandMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		p.logger.WithError(err).Errorf("decode command message failed: %s", string(payload))
		return
	}

	if p.commandProcessor == nil {
		p.logger.Error("command processor is not configured")
		return
	}

	if err := p.commandProcessor.ProcessCommand(deviceID, messageID, message); err != nil {
		p.logger.WithError(err).Errorf("process command failed: method=%s, device_id=%s, message_id=%s", message.Method, deviceID, messageID)
		return
	}
	p.logger.Infof("command processed: method=%s, device_id=%s, message_id=%s", message.Method, deviceID, messageID)
}

func (p *PlatformClient) GetCommandProcessor() CommandProcessorInterface {
	return p.commandProcessor
}

func (p *PlatformClient) GetControlProcessor() ControlProcessorInterface {
	return p.controlProcessor
}

func (p *PlatformClient) startControlSubscription() error {
	controlTopic := fmt.Sprintf("plugin/%s/devices/telemetry/control/+", p.Config.ServiceIdentifier)
	p.logger.Infof("subscribing control topic: %s", controlTopic)

	if err := p.sdkClient.MQTT().Subscribe(controlTopic, 1, p.handleControlMessage); err != nil {
		return fmt.Errorf("订阅控制主题失败: %w", err)
	}
	return nil
}

func (p *PlatformClient) handleControlMessage(topic string, payload []byte) {
	p.logger.Debugf("control message received: topic=%s, payload=%s", topic, string(payload))

	parts := strings.Split(topic, "/")
	if len(parts) != 6 {
		p.logger.Errorf("invalid control topic: %s", topic)
		return
	}

	deviceNumber := parts[5]

	var controlData map[string]interface{}
	if err := json.Unmarshal(payload, &controlData); err != nil {
		p.logger.WithError(err).Errorf("decode control message failed: %s", string(payload))
		return
	}

	if p.controlProcessor == nil {
		p.logger.Error("control processor is not configured")
		return
	}

	if err := p.controlProcessor.ProcessControl(deviceNumber, controlData); err != nil {
		p.logger.WithError(err).Errorf("process control failed: device_number=%s", deviceNumber)
		return
	}
	p.logger.Infof("control processed: device_number=%s", deviceNumber)
}
