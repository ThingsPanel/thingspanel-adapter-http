package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ThingsPanel/tp-protocol-sdk-go/client"
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
