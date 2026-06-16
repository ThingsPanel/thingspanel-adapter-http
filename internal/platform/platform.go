// internal/platform/platform.go
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

// 设备状态常量
const (
	DeviceStatusOffline = 0 // 设备离线
	DeviceStatusOnline  = 1 // 设备在线
)

// CommandProcessorInterface 指令处理器接口
type CommandProcessorInterface interface {
	ProcessCommand(deviceID, messageID string, message CommandMessage) error
}

// ControlProcessorInterface 控制处理器接口
type ControlProcessorInterface interface {
	ProcessControl(deviceNumber string, controlData map[string]interface{}) error
}

// CommandMessage 指令消息结构
type CommandMessage struct {
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// PlatformClient 平台客户端
type PlatformClient struct {
	sdkClient        *client.Client
	logger           *logrus.Logger
	deviceCache      map[string]*types.Device // key为deviceNumber
	deviceIDCache    map[string]*types.Device // key为deviceID
	deviceAddrCache  map[string]string        // key为deviceNumber, value为最近一次上报IP
	cacheMutex       sync.RWMutex
	Config           Config
	commandProcessor CommandProcessorInterface
	controlProcessor ControlProcessorInterface
}

// Config 平台配置
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

// NewPlatformClient 创建平台客户端
func NewPlatformClient(config Config, logger *logrus.Logger) (*PlatformClient, error) {
	// Normalize BaseURL: tp-protocol-sdk expects a full URL with scheme.
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

	// 打印sdkConfig
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
		deviceCache:     make(map[string]*types.Device), // key为deviceNumber
		deviceIDCache:   make(map[string]*types.Device), // key为deviceID
		deviceAddrCache: make(map[string]string),
	}, nil
}

// GetDevice 通过deviceNumber获取设备信息(带缓存)
func (p *PlatformClient) GetDevice(deviceNumber string) (*types.Device, error) {
	// 先查缓存
	p.cacheMutex.RLock()
	if device, ok := p.deviceCache[deviceNumber]; ok && device.ID != "" {
		p.cacheMutex.RUnlock()
		return device, nil
	}
	p.cacheMutex.RUnlock()

	// 缓存未命中,从平台获取
	req := &client.DeviceConfigRequest{
		DeviceNumber: deviceNumber,
	}

	resp, err := p.sdkClient.Device().GetDeviceConfig(context.Background(), req)
	if err != nil {
		return nil, err
	}

	logrus.Debugf("resp: %+v", resp)
	if resp.Data.ID == "" {
		return nil, fmt.Errorf("设备不存在: %s", deviceNumber)
	}
	logrus.Infof("设备存在: %s", resp.Data)

	// 更新缓存
	p.cacheMutex.Lock()
	p.deviceCache[deviceNumber] = &resp.Data
	p.deviceIDCache[resp.Data.ID] = &resp.Data
	p.cacheMutex.Unlock()

	return &resp.Data, nil
}

// 动态注册
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

// 子设备动态注册
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

// 网关动态注册
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

// 获取服务接入点列表
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

// ClearDeviceCache 清理指定设备的缓存
func (p *PlatformClient) ClearDeviceCache(deviceNumber string) {
	p.cacheMutex.Lock()
	// 先获取设备ID用于清理ID缓存
	if device, exists := p.deviceCache[deviceNumber]; exists {
		delete(p.deviceIDCache, device.ID)
	}
	delete(p.deviceCache, deviceNumber)
	delete(p.deviceAddrCache, deviceNumber)
	p.cacheMutex.Unlock()
	p.logger.WithField("device_number", deviceNumber).Debug("设备缓存已清理")
}

// UpdateDeviceAddress 记录设备最近一次HTTP上报来源地址。
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
	}).Debug("设备上报地址已更新")
}

// GetDeviceAddress 获取设备最近一次HTTP上报来源地址。
func (p *PlatformClient) GetDeviceAddress(deviceNumber string) (string, bool) {
	p.cacheMutex.RLock()
	address, ok := p.deviceAddrCache[deviceNumber]
	p.cacheMutex.RUnlock()
	return address, ok && address != ""
}

// GetDeviceByID 通过设备ID查找设备
func (p *PlatformClient) GetDeviceByID(deviceID string) (*types.Device, error) {
	// 先查ID缓存
	p.cacheMutex.RLock()
	if device, ok := p.deviceIDCache[deviceID]; ok && device.ID != "" {
		p.cacheMutex.RUnlock()
		logrus.Infof("设备ID找到，返回: %s", device.DeviceNumber)
		return device, nil
	}
	p.cacheMutex.RUnlock()

	logrus.Infof("设备ID未找到，去平台查: %s", deviceID)
	// 缓存未命中，从平台获取
	req := &client.DeviceConfigRequest{
		DeviceID: deviceID,
	}
	resp, err := p.sdkClient.Device().GetDeviceConfig(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("获取设备信息失败: %s", resp.Message)
	}
	// 更新缓存
	p.cacheMutex.Lock()
	p.deviceCache[resp.Data.DeviceNumber] = &resp.Data
	p.deviceIDCache[resp.Data.ID] = &resp.Data
	p.cacheMutex.Unlock()
	logrus.Infof("设备ID找到，更新缓存: %s", resp.Data.DeviceNumber)
	return &resp.Data, nil
}

// SendTelemetry 发送遥测数据
func (p *PlatformClient) SendTelemetry(deviceID string, values map[string]interface{}) error {
	// 1. 先将 values 转换为 JSON
	valuesJSON, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("序列化values失败: %v", err)
	}

	// 2. 将 JSON 进行 base64 编码
	valuesBase64 := base64.StdEncoding.EncodeToString(valuesJSON)

	// 3. 构造最终消息
	msg := map[string]interface{}{
		"device_id": deviceID,
		"values":    valuesBase64, // base64 编码的字符串
	}

	// 4. 将整个消息转换为 JSON
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %v", err)
	}

	// 5. 发送消息
	if err := p.sdkClient.MQTT().Publish("devices/telemetry", 1, string(payload)); err != nil {
		return fmt.Errorf("发送消息失败: %v", err)
	}

	p.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
	}).Debug("遥测数据发送成功", string(valuesJSON))

	return nil
}

// Close 关闭客户端
func (p *PlatformClient) Close() {
	if p.sdkClient != nil {
		p.sdkClient.Close()
	}
}

// SendDeviceStatus 发送设备状态
// status: 设备状态，0=离线，1=在线
func (p *PlatformClient) SendDeviceStatus(deviceID string, status int) error {
	// 验证状态值
	if status != DeviceStatusOffline && status != DeviceStatusOnline {
		return fmt.Errorf("无效的设备状态值: %d，只支持 0(离线) 或 1(在线)", status)
	}

	// payload
	payload := []byte(fmt.Sprintf("%d", status))
	if err := p.sdkClient.MQTT().Publish("devices/status/"+deviceID, 1, payload); err != nil {
		return fmt.Errorf("发送状态消息失败: %v", err)
	}

	statusText := "离线"
	if status == DeviceStatusOnline {
		statusText = "在线"
	}
	p.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
		"status":    statusText,
	}).Debug("设备状态已发送")

	return nil
}

// PublishCommandResponse 发布指令响应到平台
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
	}).Info("指令响应已回传平台")
	return nil
}

// SendHeartbeat 发送插件心跳
func (p *PlatformClient) SendHeartbeat(ctx context.Context, serviceIdentifier string) error {
	req := &client.HeartbeatRequest{
		ServiceIdentifier: serviceIdentifier,
	}

	resp, err := p.sdkClient.Service().SendHeartbeat(ctx, req)
	if err != nil {
		return fmt.Errorf("发送心跳失败: %v", err)
	}

	if resp.Code != 200 {
		return fmt.Errorf("心跳响应异常: code=%d, message=%s", resp.Code, resp.Message)
	}

	return nil
}

// SetCommandProcessor 设置指令处理器
func (p *PlatformClient) SetCommandProcessor(processor CommandProcessorInterface) {
	p.commandProcessor = processor
	p.logger.Info("指令处理器已设置")

	// 启动MQTT指令订阅
	if err := p.startCommandSubscription(); err != nil {
		p.logger.WithError(err).Error("启动指令订阅失败")
	}
}

// SetControlProcessor 设置控制处理器
func (p *PlatformClient) SetControlProcessor(processor ControlProcessorInterface) {
	p.controlProcessor = processor
	p.logger.Info("控制处理器已设置")

	// 启动MQTT控制订阅
	if err := p.startControlSubscription(); err != nil {
		p.logger.WithError(err).Error("启动控制订阅失败")
	}
}

// startCommandSubscription 启动MQTT指令订阅
func (p *PlatformClient) startCommandSubscription() error {
	// 订阅指令主题
	commandTopic := fmt.Sprintf("plugin/%s/devices/command/+/+", p.Config.ServiceIdentifier)

	p.logger.Infof("开始订阅指令主题: %s", commandTopic)

	if err := p.sdkClient.MQTT().Subscribe(commandTopic, 1, p.handleCommandMessage); err != nil {
		return fmt.Errorf("订阅指令主题失败: %v", err)
	}

	p.logger.Info("指令主题订阅成功")
	return nil
}

// handleCommandMessage 处理指令消息
func (p *PlatformClient) handleCommandMessage(topic string, payload []byte) {
	p.logger.Debugf("接收到指令消息: topic=%s, payload=%s", topic, string(payload))

	// 解析topic获取deviceID和messageID
	// topic格式: plugin/{service_identifier}/devices/command/{device_id}/{message_id}
	parts := strings.Split(topic, "/")
	if len(parts) != 6 {
		p.logger.Errorf("指令主题格式错误: %s", topic)
		return
	}

	deviceID := parts[4]
	messageID := parts[5]

	// 解析消息体
	var message CommandMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		p.logger.WithError(err).Errorf("解析指令消息失败: %s", string(payload))
		return
	}

	// 检查指令处理器是否已设置
	if p.commandProcessor == nil {
		p.logger.Error("指令处理器未设置，无法处理指令")
		return
	}

	// 处理指令
	if err := p.commandProcessor.ProcessCommand(deviceID, messageID, message); err != nil {
		p.logger.WithError(err).Errorf("处理指令失败: method=%s, deviceID=%s, messageID=%s",
			message.Method, deviceID, messageID)
	} else {
		p.logger.Infof("指令处理成功: method=%s, deviceID=%s, messageID=%s",
			message.Method, deviceID, messageID)
	}
}

// GetCommandProcessor 获取指令处理器
func (p *PlatformClient) GetCommandProcessor() CommandProcessorInterface {
	return p.commandProcessor
}

// GetControlProcessor 获取控制处理器
func (p *PlatformClient) GetControlProcessor() ControlProcessorInterface {
	return p.controlProcessor
}

// startControlSubscription 启动MQTT控制订阅
func (p *PlatformClient) startControlSubscription() error {
	// 订阅控制主题
	controlTopic := fmt.Sprintf("plugin/%s/devices/telemetry/control/+", p.Config.ServiceIdentifier)

	p.logger.Infof("开始订阅控制主题: %s", controlTopic)

	if err := p.sdkClient.MQTT().Subscribe(controlTopic, 1, p.handleControlMessage); err != nil {
		return fmt.Errorf("订阅控制主题失败: %v", err)
	}

	p.logger.Info("控制主题订阅成功")
	return nil
}

// handleControlMessage 处理控制消息
func (p *PlatformClient) handleControlMessage(topic string, payload []byte) {
	p.logger.Debugf("接收到控制消息: topic=%s, payload=%s", topic, string(payload))

	// 解析topic获取device_number
	// topic格式: plugin/{service_identifier}/devices/telemetry/control/{device_number}
	parts := strings.Split(topic, "/")
	if len(parts) != 6 {
		p.logger.Errorf("控制主题格式错误: %s", topic)
		return
	}

	deviceNumber := parts[5]

	// 解析消息体
	var controlData map[string]interface{}
	if err := json.Unmarshal(payload, &controlData); err != nil {
		p.logger.WithError(err).Errorf("解析控制消息失败: %s", string(payload))
		return
	}

	// 检查控制处理器是否已设置
	if p.controlProcessor == nil {
		p.logger.Error("控制处理器未设置，无法处理控制消息")
		return
	}

	// 处理控制消息
	if err := p.controlProcessor.ProcessControl(deviceNumber, controlData); err != nil {
		p.logger.WithError(err).Errorf("处理控制消息失败: device_number=%s", deviceNumber)
	} else {
		p.logger.Infof("控制消息处理成功: device_number=%s", deviceNumber)
	}
}
