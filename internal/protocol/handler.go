package protocol

import (
	"context"
	"fmt"
	"tp-plugin/internal/pkg/logger"

	"github.com/sirupsen/logrus"
)

// SingleProtocolHandler 单协议处理器
type SingleProtocolHandler struct {
	handler     ProtocolHandler
	httpHandler *HTTPHandler // Changed from tcpHandler
	platform    PlatformInterface
	logger      *logrus.Logger
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewSingleProtocolHandler 创建单协议处理器
func NewSingleProtocolHandler(handler ProtocolHandler, platform PlatformInterface, logger *logrus.Logger) *SingleProtocolHandler {
	ctx, cancel := context.WithCancel(context.Background())

	return &SingleProtocolHandler{
		handler:  handler,
		platform: platform,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动协议处理器
func (s *SingleProtocolHandler) Start() error {
	// 启动协议
	if err := s.handler.Start(); err != nil {
		return fmt.Errorf("启动协议 %s 失败: %w", s.handler.Name(), err)
	}

	// 创建并启动HTTP处理器 (Changed from TCP)
	s.httpHandler = NewHTTPHandler(s.handler.Port(), s.handler, s.platform, s.logger)
	if err := s.httpHandler.Start(); err != nil {
		s.handler.Stop()
		return fmt.Errorf("启动HTTP服务器失败: %w", err)
	}

	s.logger.Infof("协议 %s (v%s) 已启动，端口: %d", s.handler.Name(), s.handler.Version(), s.handler.Port())
	return nil
}

// Stop 停止协议处理器
func (s *SingleProtocolHandler) Stop() error {
	var lastError error

	// 取消上下文
	s.cancel()

	// 停止HTTP处理器
	if s.httpHandler != nil {
		if err := s.httpHandler.Stop(); err != nil {
			s.logger.WithError(err).Error("停止HTTP服务器失败")
			lastError = err
		}
	}

	// 停止协议
	if err := s.handler.Stop(); err != nil {
		s.logger.WithError(err).Error("停止协议失败")
		lastError = err
	}

	s.logger.Infof("协议 %s 已停止", s.handler.Name())
	return lastError
}

// GetInfo 获取协议信息
func (s *SingleProtocolHandler) GetInfo() ProtocolInfo {
	status := "running"
	if s.httpHandler == nil {
		status = "stopped"
	}

	return ProtocolInfo{
		Name:    s.handler.Name(),
		Version: s.handler.Version(),
		Port:    s.handler.Port(),
		Status:  status,
	}
}

// SendCommand 向指定设备发送指令
func (s *SingleProtocolHandler) SendCommand(deviceNumber string, cmd *Command) error {
	if s.httpHandler == nil {
		return fmt.Errorf("HTTP处理器未启动")
	}

	// Log command attempt for Http
	logger.LogDeviceCommand(deviceNumber, cmd.Action, cmd.Parameters, "sending_not_supported_on_http")
	return s.httpHandler.SendCommand(deviceNumber, cmd)
}

// GetConnectedDevices 获取已连接设备列表
func (s *SingleProtocolHandler) GetConnectedDevices() []string {
	if s.httpHandler == nil {
		return []string{}
	}

	return s.httpHandler.GetConnectedDevices()
}

// IsRunning 检查协议是否正在运行
func (s *SingleProtocolHandler) IsRunning() bool {
	return s.httpHandler != nil
}
