package downlink

import (
	"errors"
	"fmt"
	"github.com/ThingsPanel/tp-protocol-sdk-go/types"
	"github.com/sirupsen/logrus"
	"sync"
	"testing"
	"time"
)

type ackTestPlatform struct {
	mu      sync.Mutex
	calls   int
	fail    bool
	entered chan struct{}
	release chan struct{}
}

func (f *ackTestPlatform) GetDevice(string) (*types.Device, error) {
	return nil, errors.New("unexpected device lookup")
}
func (f *ackTestPlatform) GetDeviceByID(string) (*types.Device, error) {
	return nil, errors.New("unexpected device lookup")
}
func (f *ackTestPlatform) PublishCommandResponse(string, string, bool, interface{}) error {
	f.mu.Lock()
	f.calls++
	fail := f.fail
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
		<-f.release
	}
	if fail {
		return errors.New("temporary publish failure; outcome not assumed")
	}
	return nil
}
func ackFixture(f *ackTestPlatform) *Processor {
	p := NewProcessor(f, logrus.New())
	p.inflight[inflightKey("a", "m")] = QueuedMessage{Type: "command", DeviceID: "id-a", DeviceNumber: "a", MessageID: "m", ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	return p
}
func TestHTTPACKExplicitRetryAfterFailureAndIdempotentSuccess(t *testing.T) {
	f := &ackTestPlatform{fail: true}
	p := ackFixture(f)
	if err := p.AckCommand("a", "m", true, map[string]interface{}{"state": 1}); err == nil {
		t.Fatal("expected publish failure")
	}
	if _, ok := p.inflight[inflightKey("a", "m")]; !ok {
		t.Fatal("failure lost retryable inflight")
	}
	if f.calls != 1 {
		t.Fatal("automatic replay")
	}
	if err := p.AckCommand("a", "m", false, nil); err == nil {
		t.Fatal("conflicting retry accepted")
	}
	f.fail = false
	if err := p.AckCommand("a", "m", true, map[string]interface{}{"state": 1}); err != nil {
		t.Fatal(err)
	}
	if err := p.AckCommand("a", "m", true, map[string]interface{}{"state": 1}); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || len(p.inflight) != 0 {
		t.Fatal("success repeat republished or inflight leaked")
	}
	if err := p.AckCommand("a", "m", true, map[string]interface{}{"state": 2}); err == nil {
		t.Fatal("conflicting success accepted")
	}
	p.acks[inflightKey("a", "m")].retainUntil = time.Now().Add(-time.Second)
	if err := p.AckCommand("a", "m", true, map[string]interface{}{"state": 1}); err == nil {
		t.Fatal("evicted completion resurrected")
	}
	if len(p.acks) != 0 || f.calls != 2 {
		t.Fatal("expired completion retained/republished")
	}
}
func TestHTTPACKConcurrentSameRequestPublishesOnce(t *testing.T) {
	f := &ackTestPlatform{entered: make(chan struct{}, 20), release: make(chan struct{})}
	p := ackFixture(f)
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() { results <- p.AckCommand("a", "m", true, nil) }()
	}
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("no publish")
	}
	if err := p.AckCommand("a", "m", false, nil); err == nil {
		t.Fatal("concurrent conflicting ACK accepted")
	}
	close(f.release)
	for i := 0; i < 20; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("ACK waiter leaked")
		}
	}
	if f.calls != 1 {
		t.Fatalf("published %d times", f.calls)
	}
}
func TestHTTPACKFailedAttemptCannotRetryAfterDeadline(t *testing.T) {
	f := &ackTestPlatform{fail: true}
	p := ackFixture(f)
	if err := p.AckCommand("a", "m", true, nil); err == nil {
		t.Fatal("missing failure")
	}
	key := inflightKey("a", "m")
	item := p.inflight[key]
	item.ExpiresAt = 1
	p.inflight[key] = item
	p.acks[key].retainUntil = time.UnixMilli(1)
	if err := p.AckCommand("a", "m", true, nil); err == nil {
		t.Fatal("expired execution receipt forwarded")
	}
	if err := p.AckCommand("other", "m", true, nil); err == nil {
		t.Fatal("cross-device accepted")
	}
	if f.calls != 1 || len(p.inflight) != 0 || len(p.acks) != 0 {
		t.Fatal("expired state persisted or republished")
	}
}

func TestHTTPACKRetentionCapacityDoesNotLosePendingReceipt(t *testing.T) {
	f := &ackTestPlatform{}
	p := ackFixture(f)
	for i := 0; i < maxACKRecords; i++ {
		p.acks[fmt.Sprint(i)] = &ackAttempt{retainUntil: time.Now().Add(time.Minute), err: errors.New("retryable")}
	}
	if err := p.AckCommand("a", "m", true, nil); err == nil {
		t.Fatal("unbounded ACK cache")
	}
	if f.calls != 0 || len(p.inflight) != 1 {
		t.Fatal("capacity rejection published/lost receipt")
	}
	for _, record := range p.acks {
		record.retainUntil = time.Now().Add(-time.Second)
	}
	if err := p.AckCommand("a", "m", true, nil); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || len(p.acks) != 1 {
		t.Fatal("expired completion cleanup failed")
	}
}

func TestHTTPACKCompletedCapacityEvictsOldestWithoutBlockingNewACK(t *testing.T) {
	f := &ackTestPlatform{}
	p := ackFixture(f)
	base := time.Now().Add(time.Minute)
	for i := 0; i < maxACKRecords; i++ {
		p.acks[fmt.Sprint(i)] = &ackAttempt{retainUntil: base.Add(time.Duration(i) * time.Millisecond)}
	}
	active := p.acks["0"]
	active.sending = true
	active.done = make(chan struct{})
	failed := p.acks["1"]
	failed.err = errors.New("retryable")
	if err := p.AckCommand("a", "m", true, nil); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || len(p.acks) != maxACKRecords {
		t.Fatal("new ACK blocked or cache exceeded bound")
	}
	if p.acks["0"] != active || p.acks["1"] != failed {
		t.Fatal("active/failed record evicted")
	}
	if p.acks["2"] != nil {
		t.Fatal("oldest successful completion not evicted")
	}
}
