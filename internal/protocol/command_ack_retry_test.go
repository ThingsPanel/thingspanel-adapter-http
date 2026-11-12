package protocol

import (
	"errors"
	"github.com/ThingsPanel/tp-protocol-sdk-go/types"
	"github.com/sirupsen/logrus"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"tp-plugin/internal/downlink"
	"tp-plugin/internal/platform"
)

type retryHTTPPlatform struct {
	calls int
	fail  bool
}

func (*retryHTTPPlatform) ManagedRuntime() bool                               { return true }
func (*retryHTTPPlatform) SendTelemetry(string, map[string]interface{}) error { return nil }
func (*retryHTTPPlatform) SendDeviceStatus(string, int) error                 { return nil }
func (*retryHTTPPlatform) DynamicRegister(string) (*types.DeviceDynamicAuthData, error) {
	return nil, errors.New("unused")
}
func (*retryHTTPPlatform) UpdateDeviceAddress(string, string) {}
func (*retryHTTPPlatform) GetDevice(string) (*types.Device, error) {
	return &types.Device{ID: "id-a", DeviceNumber: "number-a", Voucher: `{"accessToken":"device-secret"}`}, nil
}
func (f *retryHTTPPlatform) GetDeviceByID(string) (*types.Device, error) {
	return f.GetDevice("number-a")
}
func (f *retryHTTPPlatform) PublishCommandResponse(string, string, bool, interface{}) error {
	f.calls++
	if f.fail {
		return errors.New("temporary send failure")
	}
	return nil
}

func TestHTTPACKResponseRetryAndAuthenticationWithRealQueue(t *testing.T) {
	f := &retryHTTPPlatform{fail: true}
	p := downlink.NewProcessor(f, logrus.New())
	h := NewHTTPHandler(0, nil, f, logrus.New(), false, "", p)
	if err := p.ProcessCommand("id-a", "message-a", platform.CommandMessage{Method: "set", ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	poll := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/devices/number-a/poll?timeout=1ms", nil)
	r.Header.Set("Access-Token", "device-secret")
	h.handlePoll(poll, r, "number-a")
	if poll.Code != 200 || !strings.Contains(poll.Body.String(), "message-a") {
		t.Fatal(poll.Code, poll.Body.String())
	}
	ack := func(token, body string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/v1/devices/number-a/commands/message-a/ack", strings.NewReader(body))
		r.Header.Set("Access-Token", token)
		h.handleCommandAck(w, r, "number-a", "message-a")
		return w.Code
	}
	if code := ack("wrong", `{"ok":true}`); code != 401 || f.calls != 0 {
		t.Fatal("wrong credential bypass", code)
	}
	if code := ack("device-secret", `{"ok":true,"data":{"state":1}}`); code != 500 || f.calls != 1 {
		t.Fatal("failure not surfaced", code)
	}
	f.fail = false
	for i := 0; i < 2; i++ {
		if code := ack("device-secret", `{"ok":true,"data":{"state":1}}`); code != 200 {
			t.Fatal("idempotent retry failed", code)
		}
	}
	if f.calls != 2 {
		t.Fatal("duplicate receipt republished", f.calls)
	}
	if code := ack("device-secret", `{"ok":false}`); code != 500 || f.calls != 2 {
		t.Fatal("conflicting receipt accepted", code)
	}
}
