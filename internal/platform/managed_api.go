package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ThingsPanel/tp-protocol-sdk-go/client"
)

func (p *PlatformClient) ManagedRuntime() bool { return p.Config.Managed }

// The legacy SDK has no authenticated HTTP transport option. Keep its MQTT and
// legacy behavior while using a private, bounded HTTP client for managed calls.
// Never log response bodies: device configuration contains device credentials.
func (p *PlatformClient) managedPost(ctx context.Context, path string, body, result interface{}) error {
	if !p.Config.Managed || strings.TrimSpace(p.Config.APIToken) == "" {
		return fmt.Errorf("managed identity required")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	base := strings.TrimRight(p.Config.BaseURL, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("invalid platform endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+p.Config.APIToken)
	req.Header.Set("Content-Type", "application/json")
	hc := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("platform request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("platform authorization/request rejected: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(result); err != nil {
		return fmt.Errorf("invalid platform response")
	}
	return nil
}

func (p *PlatformClient) getDeviceConfig(ctx context.Context, req *client.DeviceConfigRequest) (*client.DeviceConfigResponse, error) {
	if !p.Config.Managed {
		return p.sdkClient.Device().GetDeviceConfig(ctx, req)
	}
	var result client.DeviceConfigResponse
	err := p.managedPost(ctx, "/api/v1/plugin/device/config", req, &result)
	return &result, err
}

func (p *PlatformClient) sendHeartbeat(ctx context.Context, req *client.HeartbeatRequest) (*client.HeartbeatResponse, error) {
	if !p.Config.Managed {
		return p.sdkClient.Service().SendHeartbeat(ctx, req)
	}
	var result client.HeartbeatResponse
	err := p.managedPost(ctx, "/api/v1/plugin/heartbeat", req, &result)
	return &result, err
}
