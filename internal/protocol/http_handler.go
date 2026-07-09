package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"tp-plugin/internal/pkg/logger"

	"github.com/sirupsen/logrus"
)

// HTTPHandler HTTP Server Handler
type HTTPHandler struct {
	port         int
	handler      ProtocolHandler
	platform     PlatformInterface
	logger       *logrus.Logger
	server       *http.Server
	autoRegister bool
	httpAPIKey   string
	downlink     DownlinkPoller
}

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// HandleHealth handles health check
func (h *HTTPHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// HandleCustomFormConfig handles form config request directly
func (h *HTTPHandler) HandleCustomFormConfig(w http.ResponseWriter, r *http.Request) {
	// Simple static config that matches README structure (Array)
	// We can read from file, or just hardcode for stability if file issues persist.
	// But let's try to return the Array structure expected.

	config := []map[string]interface{}{
		{
			"dataKey":     "temp",
			"label":       "Temperature(C)",
			"placeholder": "Input Temp",
			"type":        "input",
			"validate": map[string]interface{}{
				"required": true,
				"message":  "Required",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{
		Code:    200,
		Message: "success",
		Data:    config,
	})
}

// NewHTTPHandler Create HTTP Handler
func NewHTTPHandler(port int, handler ProtocolHandler, platform PlatformInterface, logger *logrus.Logger, autoRegister bool, httpAPIKey string, downlink DownlinkPoller) *HTTPHandler {
	return &HTTPHandler{
		port:         port,
		handler:      handler,
		platform:     platform,
		logger:       logger,
		autoRegister: autoRegister,
		httpAPIKey:   httpAPIKey,
		downlink:     downlink,
	}
}

// Start Start HTTP Server
func (h *HTTPHandler) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/healthz", h.HandleHealth)
	mux.HandleFunc("/api/v1/uplink", h.handleData)
	mux.HandleFunc("/api/v1/devices/", h.handleDeviceAPI)

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
		logger.LogDeviceData("unknown", "received", body, map[string]interface{}{
			"extract_error": err.Error(),
			"remote_addr":  r.RemoteAddr,
			"protocol":     h.handler.Name(),
		})
		http.Error(w, "Failed to extract device number", http.StatusBadRequest)
		return
	}
	if remoteIP := clientIP(r); remoteIP != "" {
		h.platform.UpdateDeviceAddress(deviceNumber, remoteIP)
	}

	// 2. Get Device Info (Check if exists)
	device, err := h.platform.GetDevice(deviceNumber)
	var deviceID string
	if err != nil {
		h.logger.WithError(err).Warnf("Device not found: %s", deviceNumber)

		// Attempt Auto-Register if enabled
		if h.autoRegister {
			h.logger.Infof("Attempting auto-register for device: %s", deviceNumber)
			if _, err := h.platform.DynamicRegister(deviceNumber); err != nil {
				h.logger.WithError(err).Error("Auto-register failed")
				http.Error(w, "Device not found and registration failed", http.StatusNotFound)
				return
			}
			h.logger.Infof("Auto-register success for device: %s", deviceNumber)

			// Retry getting device
			device, err = h.platform.GetDevice(deviceNumber)
			if err != nil {
				h.logger.WithError(err).Error("Get device info failed after register")
				http.Error(w, "Device registered but failed to retrieve info", http.StatusInternalServerError)
				return
			}
		} else {
			logger.LogDeviceEvent(deviceNumber, "get_device_failed", map[string]interface{}{
				"error":         err.Error(),
				"device_number": deviceNumber,
				"protocol":      h.handler.Name(),
			})
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}
	}
	deviceID = device.ID

	// 3. Authorize using device's Access-Token from voucher
	if !h.authorizeDeviceRequest(r, device.Voucher, device.Config) {
		got := r.Header.Get("X-Api-Key")
		if got == "" {
			got = r.Header.Get("Access-Token")
		}
		h.logger.WithFields(logrus.Fields{
			"remote_addr":   r.RemoteAddr,
			"device_number": deviceNumber,
			"got_token":     got,
		}).Warn("Uplink auth failed: token mismatch")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Log received data
	logger.LogDeviceData(deviceNumber, "received", body, map[string]interface{}{
		"remote_addr": r.RemoteAddr,
		"protocol":    h.handler.Name(),
	})

	// 4. Parse Data
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

func (h *HTTPHandler) handleDeviceAPI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	deviceNumber, err := url.PathUnescape(parts[0])
	if err != nil || deviceNumber == "" {
		http.Error(w, "Invalid device number", http.StatusBadRequest)
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "poll":
		h.handlePoll(w, r, deviceNumber)
	case len(parts) == 4 && parts[1] == "commands" && parts[3] == "ack":
		messageID, err := url.PathUnescape(parts[2])
		if err != nil || messageID == "" {
			http.Error(w, "Invalid message id", http.StatusBadRequest)
			return
		}
		h.handleCommandAck(w, r, deviceNumber, messageID)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) handlePoll(w http.ResponseWriter, r *http.Request, deviceNumber string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.downlink == nil {
		http.Error(w, "Downlink polling is not enabled", http.StatusNotImplemented)
		return
	}

	device, err := h.platform.GetDevice(deviceNumber)
	if err != nil {
		h.logger.WithError(err).Warnf("poll device not found: %s", deviceNumber)
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}
	if !h.authorizeDeviceRequest(r, device.Voucher, device.Config) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	timeout := pollTimeout(r)
	items, err := h.downlink.Poll(r.Context(), deviceNumber, timeout)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		h.logger.WithError(err).Errorf("poll failed: device_number=%s", deviceNumber)
		http.Error(w, "Poll failed", http.StatusInternalServerError)
		return
	}

	_ = h.platform.SendDeviceStatus(device.ID, 1)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"commands": items,
	})
}

func (h *HTTPHandler) handleCommandAck(w http.ResponseWriter, r *http.Request, deviceNumber, messageID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.downlink == nil {
		http.Error(w, "Downlink polling is not enabled", http.StatusNotImplemented)
		return
	}

	device, err := h.platform.GetDevice(deviceNumber)
	if err != nil {
		h.logger.WithError(err).Warnf("ack device not found: %s", deviceNumber)
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}
	if !h.authorizeDeviceRequest(r, device.Voucher, device.Config) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		OK   bool        `json:"ok"`
		Data interface{} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if err := h.downlink.AckCommand(deviceNumber, messageID, req.OK, req.Data); err != nil {
		h.logger.WithError(err).Errorf("ack command failed: device_number=%s, message_id=%s", deviceNumber, messageID)
		http.Error(w, "Ack failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
}

func (h *HTTPHandler) authorizeDeviceRequest(r *http.Request, voucher string, config map[string]interface{}) bool {
	expected := firstString(config, "accessToken", "access_token", "token")
	if expected == "" {
		expected = voucherToken(voucher)
	}
	if expected == "" {
		return true
	}

	got := r.Header.Get("X-Api-Key")
	if got == "" {
		got = r.Header.Get("Access-Token")
	}
	if got == "" {
		got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return strings.TrimSpace(got) == expected
}

func pollTimeout(r *http.Request) time.Duration {
	timeout := 30 * time.Second
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	return timeout
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func voucherToken(voucher string) string {
	voucher = strings.TrimSpace(voucher)
	if voucher == "" {
		return ""
	}
	if !strings.HasPrefix(voucher, "{") {
		return voucher
	}
	var values map[string]interface{}
	if err := json.Unmarshal([]byte(voucher), &values); err != nil {
		return ""
	}
	return firstString(values, "accessToken", "access_token", "token")
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		raw, ok := m[key]
		if !ok || raw == nil {
			continue
		}
		if value, ok := raw.(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clientIP(r *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		if header == "X-Forwarded-For" {
			value = strings.TrimSpace(strings.Split(value, ",")[0])
		}
		if ip := net.ParseIP(value); ip != nil {
			return ip.String()
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	if ip := net.ParseIP(r.RemoteAddr); ip != nil {
		return ip.String()
	}
	return ""
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
