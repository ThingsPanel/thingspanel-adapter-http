package protocol

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
	"tp-plugin/internal/pkg/logger"

	"github.com/sirupsen/logrus"
)

// HTTPHandler HTTP Server Handler
type HTTPHandler struct {
	port     int
	handler  ProtocolHandler
	platform PlatformInterface
	logger   *logrus.Logger
	server   *http.Server
}

// NewHTTPHandler Create HTTP Handler
func NewHTTPHandler(port int, handler ProtocolHandler, platform PlatformInterface, logger *logrus.Logger) *HTTPHandler {
	return &HTTPHandler{
		port:     port,
		handler:  handler,
		platform: platform,
		logger:   logger,
	}
}

// Start Start HTTP Server
func (h *HTTPHandler) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/data", h.handleData)
	// Also handle root for convenience if needed, but sticking to plan
	mux.HandleFunc("/", h.handleData) // Catch-all for simple testing

	h.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", h.port),
		Handler: mux,
	}

	h.logger.Infof("HTTP Protocol %s running on port %d", h.handler.Name(), h.port)

	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.logger.WithError(err).Error("HTTP Server failed")
		}
	}()
	return nil
}

// Stop Stop HTTP Server
func (h *HTTPHandler) Stop() error {
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return h.server.Shutdown(ctx)
	}
	return nil
}

func (h *HTTPHandler) handleData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.WithError(err).Error("Failed to read body")
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 1. Extract Device Number
	deviceNumber, err := h.handler.ExtractDeviceNumber(body)
	if err != nil {
		h.logger.WithError(err).Error("Failed to extract device number")
		// Log to unknown device log?
		logger.LogDeviceData("unknown", "received", body, map[string]interface{}{
			"extract_error": err.Error(),
			"remote_addr":   r.RemoteAddr,
			"protocol":      h.handler.Name(),
		})
		http.Error(w, "Failed to extract device number", http.StatusBadRequest)
		return
	}

	// 2. Get Device Info (Check if exists)
	device, err := h.platform.GetDevice(deviceNumber)
	var deviceID string
	if err != nil {
		h.logger.WithError(err).Error("Get device info failed")
		logger.LogDeviceEvent(deviceNumber, "get_device_failed", map[string]interface{}{
			"error":         err.Error(),
			"device_number": deviceNumber,
			"protocol":      h.handler.Name(),
		})
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}
	deviceID = device.ID
	
	// Log received data
	logger.LogDeviceData(deviceNumber, "received", body, map[string]interface{}{
		"remote_addr": r.RemoteAddr,
		"protocol":    h.handler.Name(),
	})

	// 3. Parse Data
	message, err := h.handler.ParseData(body)
	if err != nil {
		h.logger.WithError(err).Error("Parse data failed")
		logger.LogDeviceEvent(deviceNumber, "parse_data_failed", map[string]interface{}{
			"error":    err.Error(),
			"data_hex": fmt.Sprintf("%x", body),
			"protocol": h.handler.Name(),
		})
		http.Error(w, "Parse data failed", http.StatusBadRequest)
		return
	}

	// Fill device info
	if message.DeviceNumber == "" {
		message.DeviceNumber = deviceNumber
	}
	if message.DeviceID == "" {
		message.DeviceID = deviceID
	}

	// 4. Send to Platform
	if err := h.platform.SendTelemetry(message.DeviceID, message.Data); err != nil {
		h.logger.WithError(err).Error("Send to platform failed")
		logger.LogDeviceEvent(deviceNumber, "platform_send_failed", map[string]interface{}{
			"error":        err.Error(),
			"message_type": message.MessageType,
			"protocol":     h.handler.Name(),
		})
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Log success
	logger.LogDeviceEvent(deviceNumber, "platform_sent", map[string]interface{}{
		"message_type": message.MessageType,
		"data_fields":  len(message.Data),
		"protocol":     h.handler.Name(),
	})

	// 5. Update Status (Online) - Since HTTP is connectionless, we might just ping online status
	// Best effort status update
	h.platform.SendDeviceStatus(deviceID, 1)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// SendCommand - HTTP is usually request-response, so async command push is tricky.
// We can implement it if needed, but for now return error/not supported or log it.
func (h *HTTPHandler) SendCommand(deviceNumber string, cmd *Command) error {
	return fmt.Errorf("HTTP protocol does not support async commands yet")
}

func (h *HTTPHandler) GetConnectedDevices() []string {
	// HTTP is stateless, so no permanent connections
	return []string{}
}
