package examples

import (
	"encoding/json"
	"fmt"
	"time"

	"tp-plugin/internal/protocol"

	"github.com/sirupsen/logrus"
)

// HTTPDemoHandler Demo HTTP Protocol Handler
// Expects JSON payload: {"device_number": "...", "temperature": 20, ...}
type HTTPDemoHandler struct {
	port int
}

func NewHTTPDemoHandler(port int) *HTTPDemoHandler {
	return &HTTPDemoHandler{port: port}
}

func (h *HTTPDemoHandler) Name() string {
	return "HTTPDemoProtocol"
}

func (h *HTTPDemoHandler) Version() string {
	return "1.0.0"
}

func (h *HTTPDemoHandler) Port() int {
	return h.port
}

// Data Payload structure for extraction
type Payload struct {
	DeviceNumber string                 `json:"device_number"`
	Data         map[string]interface{} `json:"-"` // Remaining fields
}

func (h *HTTPDemoHandler) ExtractDeviceNumber(data []byte) (string, error) {
	var p map[string]interface{}
	if err := json.Unmarshal(data, &p); err != nil {
		return "", fmt.Errorf("invalid json: %v", err)
	}

	if v, ok := p["device_number"]; ok {
		return fmt.Sprintf("%v", v), nil
	}
	return "", fmt.Errorf("device_number missing")
}

func (h *HTTPDemoHandler) ParseData(data []byte) (*protocol.Message, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	dn, ok := payload["device_number"]
	if !ok {
		return nil, fmt.Errorf("device_number missing")
	}
	deviceNumber := fmt.Sprintf("%v", dn)

	// Remove device_number from data map to avoid duplication or keep it?
	// Usually keep it or clean it. Let's keep it for now as it doesn't hurt.

	return &protocol.Message{
		DeviceNumber: deviceNumber,
		MessageType:  "data",
		Timestamp:    time.Now(),
		Data:         payload,
		Quality:      1,
	}, nil
}

func (h *HTTPDemoHandler) EncodeCommand(cmd *protocol.Command) ([]byte, error) {
	return nil, fmt.Errorf("commands not supported via HTTP response yet")
}

func (h *HTTPDemoHandler) Start() error {
	logrus.Infof("HTTP Demo Protocol started on port %d", h.port)
	return nil
}

func (h *HTTPDemoHandler) Stop() error {
	return nil
}
