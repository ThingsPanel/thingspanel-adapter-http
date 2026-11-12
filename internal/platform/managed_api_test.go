package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encoding/json"
	"github.com/ThingsPanel/tp-protocol-sdk-go/client"
	"github.com/ThingsPanel/tp-protocol-sdk-go/types"
	"github.com/sirupsen/logrus"
)

func TestManagedAPIIdentityAndNoFallback(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer instance-secret" {
			t.Error("runtime identity missing")
		}
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"data":{"id":"device-a"}}`))
			return
		}
		w.WriteHeader(503)
		w.Write([]byte("private-device-credential"))
	}))
	defer s.Close()
	p := &PlatformClient{Config: Config{Managed: true, APIToken: "instance-secret", BaseURL: s.URL}}
	resp, err := p.getDeviceConfig(context.Background(), &client.DeviceConfigRequest{DeviceNumber: "D001"})
	if err != nil || resp.Data.ID != "device-a" {
		t.Fatalf("first lookup: %v", err)
	}
	_, err = p.getDeviceConfig(context.Background(), &client.DeviceConfigRequest{DeviceNumber: "D001"})
	if err == nil || strings.Contains(err.Error(), "private-device-credential") || calls != 2 {
		t.Fatalf("must reject without fallback or credential logging: %v", err)
	}
	p.Config.APIToken = ""
	if _, err = p.getDeviceConfig(context.Background(), &client.DeviceConfigRequest{}); err == nil || calls != 2 {
		t.Fatal("missing identity must not send request")
	}
	if _, err = p.DynamicRegister("D002"); err == nil {
		t.Fatal("managed dynamic registration must be disabled")
	}
}

func TestManagedLookupKeepsIdentityChecksAfterNumberResolution(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req client.DeviceConfigRequest
		json.NewDecoder(r.Body).Decode(&req)
		if calls == 1 && req.DeviceNumber != "D001" {
			t.Error("first lookup must resolve number")
		}
		if calls > 1 && (req.DeviceID != "device-a" || req.DeviceNumber != "") {
			t.Error("known identity must be looked up freshly by ID")
		}
		if calls == 3 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "data": map[string]interface{}{"id": "device-a", "device_number": "D001", "voucher": string(rune('a' + calls))}})
	}))
	defer s.Close()
	p := &PlatformClient{Config: Config{Managed: true, APIToken: "identity", BaseURL: s.URL}, logger: logrus.New(), deviceCache: map[string]*types.Device{}, deviceIDCache: map[string]*types.Device{}, deviceAddrCache: map[string]string{}}
	a, err := p.GetDevice("D001")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.GetDevice("D001")
	if err != nil || a.Voucher == b.Voucher {
		t.Fatal("device credentials must be refreshed")
	}
	if _, err = p.GetDevice("D001"); err == nil || calls != 3 {
		t.Fatal("revoked identity must not reuse cached configuration")
	}
}
