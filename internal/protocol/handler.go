package protocol

import (
	"context"
	"fmt"
	"tp-plugin/internal/pkg/logger"

	"github.com/sirupsen/logrus"
)

type SingleProtocolHandler struct {
	handler      ProtocolHandler
	platform     PlatformInterface
	logger       *logrus.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	autoRegister bool
	httpAPIKey   string
	downlink     DownlinkPoller
	httpHandler  *HTTPHandler
}

func NewSingleProtocolHandler(handler ProtocolHandler, platform PlatformInterface, logger *logrus.Logger, autoRegister bool, httpAPIKey string, downlink DownlinkPoller) *SingleProtocolHandler {
	ctx, cancel := context.WithCancel(context.Background())

	return &SingleProtocolHandler{
		handler:      handler,
		platform:     platform,
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
		autoRegister: autoRegister,
		httpAPIKey:   httpAPIKey,
		downlink:     downlink,
	}
}

func (s *SingleProtocolHandler) Start() error {
	if err := s.handler.Start(); err != nil {
		return fmt.Errorf("start protocol %s failed: %w", s.handler.Name(), err)
	}

	s.httpHandler = NewHTTPHandler(s.handler.Port(), s.handler, s.platform, s.logger, s.autoRegister, s.httpAPIKey, s.downlink)
	if err := s.httpHandler.Start(); err != nil {
		_ = s.handler.Stop()
		return fmt.Errorf("start HTTP server failed: %w", err)
	}

	s.logger.Infof("protocol %s (v%s) started on port %d", s.handler.Name(), s.handler.Version(), s.handler.Port())
	return nil
}

func (s *SingleProtocolHandler) Stop() error {
	var lastError error
	s.cancel()

	if s.httpHandler != nil {
		if err := s.httpHandler.Stop(); err != nil {
			s.logger.WithError(err).Error("stop HTTP server failed")
			lastError = err
		}
	}

	if err := s.handler.Stop(); err != nil {
		s.logger.WithError(err).Error("stop protocol failed")
		lastError = err
	}

	s.logger.Infof("protocol %s stopped", s.handler.Name())
	return lastError
}

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

func (s *SingleProtocolHandler) SendCommand(deviceNumber string, cmd *Command) error {
	if s.httpHandler == nil {
		return fmt.Errorf("HTTP handler is not started")
	}

	logger.LogDeviceCommand(deviceNumber, cmd.Action, cmd.Parameters, "sending_not_supported_on_http")
	return s.httpHandler.SendCommand(deviceNumber, cmd)
}

func (s *SingleProtocolHandler) GetConnectedDevices() []string {
	if s.httpHandler == nil {
		return []string{}
	}
	return s.httpHandler.GetConnectedDevices()
}

func (s *SingleProtocolHandler) IsRunning() bool {
	return s.httpHandler != nil
}
