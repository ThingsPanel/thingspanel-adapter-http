package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// UplinkRequest: minimal HTTP telemetry uplink format.
//
// Example:
//
//	{
//	  "device_number": "A001",
//	  "ts": 1730000000,
//	  "values": {"temp": 12.3, "hum": 40}
//	}
//
// Auth: header X-Api-Key must match server.http_api_key.
//
// Notes:
// - ts optional; if missing, server time is used.
// - values must be an object.
// - device_number required.
type UplinkRequest struct {
	DeviceNumber string                 `json:"device_number"`
	TS           int64                  `json:"ts,omitempty"`
	Values       map[string]interface{} `json:"values"`
}

type UplinkResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (h *HTTPHandler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *HTTPHandler) HandleUplink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeJSON(w, http.StatusMethodNotAllowed, UplinkResponse{Code: 405, Message: "method not allowed"})
		return
	}

	if h.httpAPIKey != "" {
		got := r.Header.Get("X-Api-Key")
		if got == "" || got != h.httpAPIKey {
			h.writeJSON(w, http.StatusUnauthorized, UplinkResponse{Code: 401, Message: "unauthorized"})
			return
		}
	}

	var raw map[string]interface{}
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		h.logger.WithError(err).Warn("uplink: invalid json")
		h.writeJSON(w, http.StatusBadRequest, UplinkResponse{Code: 400, Message: "invalid json"})
		return
	}
	h.logger.Infof("DEBUG: Raw JSON: %+v", raw)

	deviceNumber, ok := raw["device_number"].(string)
	if !ok || deviceNumber == "" {
		h.writeJSON(w, http.StatusBadRequest, UplinkResponse{Code: 400, Message: "device_number required"})
		return
	}

	// Extract values (all fields except device_number and ts)
	values := make(map[string]interface{})
	var ts int64

	// Check if "values" key exists and is a map (Nested format support)
	if v, ok := raw["values"].(map[string]interface{}); ok {
		values = v
	} else {
		// Flat format support
		for k, v := range raw {
			if k == "device_number" {
				continue
			}
			if k == "ts" {
				if t, ok := v.(json.Number); ok {
					if val, err := t.Int64(); err == nil {
						ts = val
						continue
					}
				} else if t, ok := v.(float64); ok {
					ts = int64(t)
					continue
				}
			}
			values[k] = v
		}
	}

	if len(values) == 0 {
		h.writeJSON(w, http.StatusBadRequest, UplinkResponse{Code: 400, Message: "values required"})
		return
	}

	req := UplinkRequest{
		DeviceNumber: deviceNumber,
		TS:           ts,
		Values:       values,
	}

	device, err := h.platform.GetDevice(req.DeviceNumber)
	if err != nil {
		if h.autoRegister {
			h.logger.WithField("device_number", req.DeviceNumber).Info("uplink: device not found, auto-register")
			_, regErr := h.platform.DynamicRegister(req.DeviceNumber)
			if regErr != nil {
				h.logger.WithError(regErr).Error("uplink: auto-register failed")
				h.writeJSON(w, http.StatusBadGateway, UplinkResponse{Code: 502, Message: "auto-register failed"})
				return
			}
			device, err = h.platform.GetDevice(req.DeviceNumber)
		}
	}
	if err != nil {
		h.logger.WithError(err).Error("uplink: get device failed")
		h.writeJSON(w, http.StatusNotFound, UplinkResponse{Code: 404, Message: "device not found"})
		return
	}

	if sendErr := h.platform.SendTelemetry(device.ID, req.Values); sendErr != nil {
		h.logger.WithError(sendErr).Error("uplink: send telemetry failed")
		h.writeJSON(w, http.StatusBadGateway, UplinkResponse{Code: 502, Message: "send telemetry failed"})
		return
	}

	// Use TS from request or current time
	finalTS := req.TS
	if finalTS == 0 {
		finalTS = time.Now().Unix()
	}
	h.logger.WithFields(logrus.Fields{
		"device_number": req.DeviceNumber,
		"device_id":     device.ID,
		"ts":            finalTS,
		"keys":          len(req.Values),
	}).Info("uplink ok")

	h.writeJSON(w, http.StatusOK, UplinkResponse{Code: 200, Message: "ok"})
}

func (h *HTTPHandler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
