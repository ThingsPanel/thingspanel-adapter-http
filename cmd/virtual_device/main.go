package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type VirtualSensor struct {
	deviceNumber string
	uplinkURL    string
	pluginBase   string
	accessToken  string
	httpClient   *http.Client
	temperature  float64
	humidity     float64
	status       string
	reportPeriod time.Duration
	pollTimeout  time.Duration
}

type PollResponse struct {
	Commands []DownlinkMessage `json:"commands"`
}

type DownlinkMessage struct {
	Type         string          `json:"type"`
	DeviceNumber string          `json:"device_number"`
	MessageID    string          `json:"message_id"`
	Method       string          `json:"method"`
	Params       json.RawMessage `json:"params"`
	Values       json.RawMessage `json:"values"`
}

func NewVirtualSensor(deviceNumber, uplinkURL, pluginBase, accessToken string, reportPeriod, pollTimeout time.Duration) *VirtualSensor {
	return &VirtualSensor{
		deviceNumber: deviceNumber,
		uplinkURL:    uplinkURL,
		pluginBase:   strings.TrimRight(pluginBase, "/"),
		accessToken:  accessToken,
		httpClient: &http.Client{
			Timeout: pollTimeout + 5*time.Second,
		},
		temperature:  20.0 + rand.Float64()*10.0,
		humidity:     40.0 + rand.Float64()*30.0,
		status:       "active",
		reportPeriod: reportPeriod,
		pollTimeout:  pollTimeout,
	}
}

func (v *VirtualSensor) Start(ctx context.Context, enablePoll bool) {
	go v.reportLoop(ctx)
	if enablePoll {
		go v.pollLoop(ctx)
	}
}

func (v *VirtualSensor) reportLoop(ctx context.Context) {
	ticker := time.NewTicker(v.reportPeriod)
	defer ticker.Stop()

	log.Printf("device %s started, report period=%v", v.deviceNumber, v.reportPeriod)
	if err := v.sendData(); err != nil {
		log.Printf("device %s first report failed: %v", v.deviceNumber, err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := v.sendData(); err != nil {
				log.Printf("device %s report failed: %v", v.deviceNumber, err)
			}
		}
	}
}

func (v *VirtualSensor) pollLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		commands, err := v.pollOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("device %s poll failed: %v", v.deviceNumber, err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, cmd := range commands {
			v.handleDownlink(ctx, cmd, "poll")
		}
	}
}

func (v *VirtualSensor) pollOnce(ctx context.Context) ([]DownlinkMessage, error) {
	endpoint := fmt.Sprintf("%s/api/v1/devices/%s/poll?timeout=%s",
		v.pluginBase,
		url.PathEscape(v.deviceNumber),
		url.QueryEscape(v.pollTimeout.String()),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create poll request failed: %w", err)
	}
	v.setAuth(req)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll HTTP failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("poll status=%s, body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var pollResp PollResponse
	if err := json.Unmarshal(respBody, &pollResp); err != nil {
		return nil, fmt.Errorf("decode poll response failed: %w, body=%s", err, strings.TrimSpace(string(respBody)))
	}
	if len(pollResp.Commands) == 0 {
		log.Printf("device %s poll timeout: no command", v.deviceNumber)
	}
	return pollResp.Commands, nil
}

func (v *VirtualSensor) sendData() error {
	v.updateSensorValues()

	payload := map[string]interface{}{
		"device_number": v.deviceNumber,
		"temp":          round(v.temperature, 1),
		"hum":           round(v.humidity, 1),
		"status":        v.status,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal report failed: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, v.uplinkURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create report request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	v.setAuth(req)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("report HTTP failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("report status=%s, body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	log.Printf("device %s reported: %s", v.deviceNumber, string(body))
	return nil
}

func (v *VirtualSensor) handleDownlink(ctx context.Context, msg DownlinkMessage, source string) (bool, interface{}) {
	var data interface{}
	switch msg.Type {
	case "command":
		data = map[string]interface{}{
			"source":  source,
			"method":  msg.Method,
			"params":  jsonObject(msg.Params),
			"handled": true,
		}
		log.Printf("device %s command received from %s: message_id=%s method=%s params=%s", v.deviceNumber, source, msg.MessageID, msg.Method, stringOrNull(msg.Params))
		if source == "poll" && msg.MessageID != "" {
			if err := v.ackCommand(ctx, msg.MessageID, true, data); err != nil {
				log.Printf("device %s ack failed: message_id=%s error=%v", v.deviceNumber, msg.MessageID, err)
			}
		}
	case "control":
		data = map[string]interface{}{
			"source":  source,
			"values":  jsonObject(msg.Values),
			"handled": true,
		}
		log.Printf("device %s control received from %s: values=%s", v.deviceNumber, source, stringOrNull(msg.Values))
	default:
		data = map[string]interface{}{
			"source":  source,
			"type":    msg.Type,
			"handled": false,
		}
		log.Printf("device %s unknown downlink from %s: type=%s", v.deviceNumber, source, msg.Type)
		return false, data
	}
	return true, data
}

func (v *VirtualSensor) ackCommand(ctx context.Context, messageID string, ok bool, data interface{}) error {
	endpoint := fmt.Sprintf("%s/api/v1/devices/%s/commands/%s/ack",
		v.pluginBase,
		url.PathEscape(v.deviceNumber),
		url.PathEscape(messageID),
	)
	body, err := json.Marshal(map[string]interface{}{
		"ok":   ok,
		"data": data,
	})
	if err != nil {
		return fmt.Errorf("marshal ack failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create ack request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	v.setAuth(req)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ack HTTP failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("ack status=%s, body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	log.Printf("device %s ack sent: message_id=%s ok=%v", v.deviceNumber, messageID, ok)
	return nil
}

func (v *VirtualSensor) setAuth(req *http.Request) {
	if v.accessToken != "" {
		req.Header.Set("Access-Token", v.accessToken)
		req.Header.Set("X-Api-Key", v.accessToken)
	}
}

func (v *VirtualSensor) updateSensorValues() {
	v.temperature += rand.Float64() - 0.5
	if v.temperature < -40.0 {
		v.temperature = -40.0
	} else if v.temperature > 85.0 {
		v.temperature = 85.0
	}

	v.humidity += (rand.Float64() - 0.5) * 4.0
	if v.humidity < 0.0 {
		v.humidity = 0.0
	} else if v.humidity > 100.0 {
		v.humidity = 100.0
	}
}

type DeviceRegistry struct {
	mu      sync.RWMutex
	devices map[string]*VirtualSensor
}

func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{devices: make(map[string]*VirtualSensor)}
}

func (r *DeviceRegistry) Add(device *VirtualSensor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[device.deviceNumber] = device
}

func (r *DeviceRegistry) Get(deviceNumber string) (*VirtualSensor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	device, ok := r.devices[deviceNumber]
	return device, ok
}

func (r *DeviceRegistry) First() (*VirtualSensor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, device := range r.devices {
		return device, true
	}
	return nil, false
}

func startDirectServer(addr string, registry *DeviceRegistry) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/command", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			DeviceNumber string          `json:"tp_device_number"`
			MessageID    string          `json:"tp_message_id"`
			Method       string          `json:"method"`
			Params       json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		device, ok := registry.First()
		if req.DeviceNumber != "" {
			device, ok = registry.Get(req.DeviceNumber)
		}
		if !ok {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}

		okResult, data := device.handleDownlink(r.Context(), DownlinkMessage{
			Type:      "command",
			MessageID: req.MessageID,
			Method:    req.Method,
			Params:    req.Params,
		}, "direct")

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message_id": req.MessageID,
			"ok":         okResult,
			"data":       data,
		})
	})

	mux.HandleFunc("/api/v1/control", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			DeviceNumber string          `json:"tp_device_number"`
			Values       json.RawMessage `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		device, ok := registry.Get(req.DeviceNumber)
		if !ok {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}

		okResult, data := device.handleDownlink(r.Context(), DownlinkMessage{
			Type:   "control",
			Values: req.Values,
		}, "direct")

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":   okResult,
			"data": data,
		})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	go func() {
		log.Printf("direct downlink receiver listening on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("direct downlink receiver failed: %v", err)
		}
	}()
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func pluginBaseFromUplink(uplinkURL string) string {
	u, err := url.Parse(uplinkURL)
	if err != nil {
		return "http://127.0.0.1:19090"
	}
	u.Path = strings.TrimSuffix(u.Path, "/api/v1/uplink")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func jsonObject(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{}
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return string(raw)
	}
	return out
}

func stringOrNull(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	return string(raw)
}

func round(v float64, precision int) float64 {
	base := 1.0
	for i := 0; i < precision; i++ {
		base *= 10
	}
	return float64(int(v*base+0.5)) / base
}

func nextDeviceNumber(start string, offset int) string {
	if offset == 0 {
		return start
	}

	prefixEnd := len(start)
	for prefixEnd > 0 && start[prefixEnd-1] >= '0' && start[prefixEnd-1] <= '9' {
		prefixEnd--
	}
	if prefixEnd == len(start) {
		return fmt.Sprintf("%s-%d", start, offset+1)
	}

	prefix := start[:prefixEnd]
	numberPart := start[prefixEnd:]
	var number int
	if _, err := fmt.Sscanf(numberPart, "%d", &number); err != nil {
		return fmt.Sprintf("%s-%d", start, offset+1)
	}
	width := len(numberPart)
	return fmt.Sprintf("%s%0*d", prefix, width, number+offset)
}

func main() {
	// 47.92.253.145
	var (
		uplinkURL    = flag.String("server", "http://127.0.0.1:19090/api/v1/uplink", "device uplink URL")
		pluginBase   = flag.String("plugin", "", "plugin base URL for poll/ack, default derived from -server")
		deviceNumber = flag.String("device", "D001", "device_number")
		accessToken  = flag.String("token", "aaaabbbb", "Access-Token")
		period       = flag.Duration("period", 10*time.Second, "report period")
		poll         = flag.Bool("poll", true, "enable long polling")
		pollTimeout  = flag.Duration("poll-timeout", 30*time.Second, "long poll timeout, max 30s")
		deviceCount  = flag.Int("count", 1, "virtual device count")
		listen       = flag.String("listen", "", "optional direct downlink listen address, for example :8080")
	)
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	if *pluginBase == "" {
		*pluginBase = pluginBaseFromUplink(*uplinkURL)
	}
	if *pollTimeout <= 0 || *pollTimeout > 30*time.Second {
		*pollTimeout = 30 * time.Second
	}

	ctx := context.Background()
	registry := NewDeviceRegistry()

	log.Printf("starting virtual HTTP sensors")
	log.Printf("uplink URL: %s", *uplinkURL)
	log.Printf("plugin base URL: %s", *pluginBase)
	log.Printf("start device number: %s", *deviceNumber)
	log.Printf("device count: %d", *deviceCount)
	log.Printf("report period: %v", *period)
	log.Printf("long polling enabled: %v", *poll)

	for i := 0; i < *deviceCount; i++ {
		currentDeviceNumber := nextDeviceNumber(*deviceNumber, i)
		sensor := NewVirtualSensor(currentDeviceNumber, *uplinkURL, *pluginBase, *accessToken, *period, *pollTimeout)
		registry.Add(sensor)
		sensor.Start(ctx, *poll)
		time.Sleep(time.Duration(i) * time.Second)
	}

	if *listen != "" {
		startDirectServer(*listen, registry)
	}

	select {}
}
