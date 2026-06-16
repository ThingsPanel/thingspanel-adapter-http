package protocol

import (
	"time"

	"github.com/ThingsPanel/tp-protocol-sdk-go/types"
)

// PlatformInterface 平台接口，避免循环依赖
type PlatformInterface interface {
	SendTelemetry(deviceID string, values map[string]interface{}) error
	SendDeviceStatus(deviceID string, status int) error
	GetDevice(deviceNumber string) (*types.Device, error)
	DynamicRegister(deviceNumber string) (*types.DeviceDynamicAuthData, error)
	UpdateDeviceAddress(deviceNumber, address string)
}

// Message 设备消息结构
type Message struct {
	DeviceNumber string                 `json:"device_number"` // 设备编号(从设备中提取)
	DeviceID     string                 `json:"device_id"`     // 设备ID(平台分配的ID)
	MessageType  string                 `json:"message_type"`  // data/heartbeat/status
	Timestamp    time.Time              `json:"timestamp"`
	Data         map[string]interface{} `json:"data"`    // 设备数据
	Quality      int                    `json:"quality"` // 数据质量，1=正常
}

// Command 设备指令结构
type Command struct {
	DeviceNumber string        `json:"device_number"` // 设备编号(从设备中提取)
	DeviceID     string        `json:"device_id"`     // 设备ID(平台分配的ID)
	CommandID    string        `json:"command_id"`
	Action       string        `json:"action"` // sleep/config/query等
	Parameters   interface{}   `json:"parameters"`
	Timeout      time.Duration `json:"timeout"`
}

// ProtocolHandler 协议处理器接口
// 90%的协议只需要实现这个接口
type ProtocolHandler interface {
	// 协议基本信息
	Name() string    // 协议名称
	Version() string // 协议版本
	Port() int       // 协议端口

	// 核心功能 - 只需要这三个方法！
	ExtractDeviceNumber(data []byte) (string, error) // 从数据包提取设备编号
	ParseData(data []byte) (*Message, error)         // 解析设备数据
	EncodeCommand(cmd *Command) ([]byte, error)      // 编码控制指令（可选）

	// 生命周期管理（通常只需要返回nil）
	Start() error // 启动协议
	Stop() error  // 停止协议
}

// ProtocolInfo 协议信息
type ProtocolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
	Status  string `json:"status"` // running/stopped/error
}
