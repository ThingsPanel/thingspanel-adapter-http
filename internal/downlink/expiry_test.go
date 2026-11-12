package downlink

import (
	"context"
	"encoding/json"
	"github.com/ThingsPanel/tp-protocol-sdk-go/types"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"tp-plugin/internal/platform"
)

type expiryPlatform struct {
	device  *types.Device
	replies []string
}

func (f *expiryPlatform) GetDevice(string) (*types.Device, error)     { return f.device, nil }
func (f *expiryPlatform) GetDeviceByID(string) (*types.Device, error) { return f.device, nil }
func (f *expiryPlatform) PublishCommandResponse(d, m string, ok bool, data interface{}) error {
	f.replies = append(f.replies, d+":"+m)
	return nil
}
func fixtureExpiry() (*Processor, *expiryPlatform) {
	f := &expiryPlatform{device: &types.Device{ID: "id-a", DeviceNumber: "number-a"}}
	return NewProcessor(f, logrus.New()), f
}
func TestHTTPCommandExpiryBoundary(t *testing.T) {
	now := time.UnixMilli(1000)
	for _, tt := range []struct {
		deadline int64
		expired  bool
	}{{0, true}, {999, true}, {1000, true}, {1001, false}} {
		if got := (QueuedMessage{Type: "command", ExpiresAt: tt.deadline}).expired(now); got != tt.expired {
			t.Fatalf("deadline %d expired=%v", tt.deadline, got)
		}
	}
	if (QueuedMessage{Type: "control"}).expired(now) {
		t.Fatal("legacy control changed")
	}
}
func TestHTTPPollMixedQueueDropsExpiredAndKeepsNormalACK(t *testing.T) {
	p, f := fixtureExpiry()
	future := time.Now().Add(time.Minute).UnixMilli()
	if err := p.ProcessCommand("id-a", "live", platform.CommandMessage{Method: "set", ExpiresAt: future}); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.queues["number-a"] = append([]QueuedMessage{{Type: "command", MessageID: "expired", ExpiresAt: 1}, {Type: "command", MessageID: "missing-deadline"}}, p.queues["number-a"]...)
	p.queues["number-b"] = []QueuedMessage{{Type: "command", MessageID: "stale-other", ExpiresAt: 1}}
	p.mu.Unlock()
	items, err := p.Poll(context.Background(), "number-a", time.Millisecond)
	if err != nil || len(items) != 1 || items[0].MessageID != "live" || items[0].ExpiresAt != future {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if len(p.queues) != 0 || len(p.inflight) != 1 {
		t.Fatal("expired entries retained")
	}
	if err := p.AckCommand("number-b", "live", true, nil); err == nil {
		t.Fatal("other device ACK accepted")
	}
	if len(f.replies) != 0 {
		t.Fatal("rejected ACK forwarded")
	}
	if err := p.AckCommand("number-a", "live", true, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.replies) != 1 || f.replies[0] != "id-a:live" {
		t.Fatal(f.replies)
	}
	if err := p.AckCommand("number-a", "live", true, nil); err != nil || len(f.replies) != 1 {
		t.Fatal("duplicate ACK must succeed without republishing", err)
	}
}
func TestHTTPExpiredOnlyAndLateACK(t *testing.T) {
	p, f := fixtureExpiry()
	for _, deadline := range []int64{0, 1} {
		if err := p.ProcessCommand("id-a", "old", platform.CommandMessage{ExpiresAt: deadline}); err == nil {
			t.Fatal("expired admitted")
		}
	}
	p.queues["number-a"] = []QueuedMessage{{Type: "command", MessageID: "old", ExpiresAt: 1}}
	p.inflight[inflightKey("number-a", "late")] = QueuedMessage{Type: "command", MessageID: "late", ExpiresAt: 1}
	items, err := p.Poll(context.Background(), "number-a", time.Millisecond)
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	if err := p.AckCommand("number-a", "late", true, nil); err == nil {
		t.Fatal("late ACK accepted")
	}
	if len(f.replies) != 0 || len(p.inflight) != 0 || len(p.queues) != 0 {
		t.Fatal("expired state/receipt retained")
	}
}
func TestHTTPDirectDeadlinePropagatedAndApplied(t *testing.T) {
	p, f := fixtureExpiry()
	deadline := time.Now().Add(time.Minute).UnixMilli()
	seen := int64(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpiresAt int64 `json:"expires_at"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = body.ExpiresAt
		_, _ = w.Write([]byte(`{"message_id":"direct","ok":true}`))
	}))
	defer server.Close()
	f.device.Config = map[string]interface{}{"downlink_host": server.URL}
	if err := p.ProcessCommand("id-a", "direct", platform.CommandMessage{Method: "set", ExpiresAt: deadline}); err != nil {
		t.Fatal(err)
	}
	if seen != deadline || len(f.replies) != 1 || len(p.queues) != 0 {
		t.Fatal("direct deadline/receipt mismatch")
	}
	if err := p.AckCommand("number-a", "direct", true, nil); err != nil || len(f.replies) != 1 {
		t.Fatal("direct ACK retry must not republish", err)
	}
}

func TestHTTPDirectTimeoutDoesNotRequeueExpiredCommand(t *testing.T) {
	p, f := fixtureExpiry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte(`{"message_id":"slow","ok":true}`))
	}))
	defer server.Close()
	f.device.Config = map[string]interface{}{"downlink_host": server.URL}
	err := p.ProcessCommand("id-a", "slow", platform.CommandMessage{Method: "set", ExpiresAt: time.Now().Add(60 * time.Millisecond).UnixMilli()})
	if err == nil || len(f.replies) != 0 || len(p.queues) != 0 || len(p.inflight) != 0 {
		t.Fatalf("expired direct command retained or acknowledged: err=%v", err)
	}
}
