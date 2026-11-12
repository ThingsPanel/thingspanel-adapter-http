package protocol

import (
	"net/http/httptest"
	"testing"
	"tp-plugin/internal/platform"
)

func TestManagedDeviceRequiresOwnCredential(t *testing.T) {
	h := &HTTPHandler{platform: &platform.PlatformClient{Config: platform.Config{Managed: true}}}
	r := httptest.NewRequest("POST", "/api/v1/uplink", nil)
	if h.authorizeDeviceRequest(r, "", nil) {
		t.Fatal("managed device without a credential must be denied")
	}
	r.Header.Set("Access-Token", "other-device-token")
	if h.authorizeDeviceRequest(r, `{"accessToken":"device-secret"}`, nil) {
		t.Fatal("wrong device token accepted")
	}
	r.Header.Set("Access-Token", "device-secret")
	if !h.authorizeDeviceRequest(r, `{"accessToken":"device-secret"}`, nil) {
		t.Fatal("valid device token rejected")
	}
}
