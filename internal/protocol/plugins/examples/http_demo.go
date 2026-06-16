package examples

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}

	dn, ok := payload["device_number"]
	if !ok {
		return nil, fmt.Errorf("device_number missing")
	}
	deviceNumber := fmt.Sprintf("%v", dn)

	values := make(map[string]interface{})
	if nested, ok := payload["values"].(map[string]interface{}); ok {
		for k, v := range nested {
			values[k] = normalizeJSONNumber(v)
		}
	} else {
		for k, v := range payload {
			if k == "device_number" || k == "ts" {
				continue
			}
			values[k] = normalizeJSONNumber(v)
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("values missing")
	}

	timestamp := time.Now()
	if rawTS, ok := payload["ts"]; ok {
		if ts, ok := parseUnixTimestamp(rawTS); ok {
			timestamp = time.Unix(ts, 0)
		}
	}

	return &protocol.Message{
		DeviceNumber: deviceNumber,
		MessageType:  "data",
		Timestamp:    timestamp,
		Data:         values,
		Quality:      1,
	}, nil
}

func normalizeJSONNumber(v interface{}) interface{} {
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return n.String()
}

func parseUnixTimestamp(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case json.Number:
		ts, err := t.Int64()
		return ts, err == nil
	case float64:
		return int64(t), true
	case string:
		ts, err := strconv.ParseInt(t, 10, 64)
		return ts, err == nil
	default:
		return 0, false
	}
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
