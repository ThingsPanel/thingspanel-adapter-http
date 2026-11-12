package platform

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type managedDevice struct {
	ID     string `json:"id"`
	Number string `json:"device_number"`
}

func scopedTopics(service string, d managedDevice) (string, string, string, error) {
	if service != "HTTP" || d.ID == "" || d.Number == "" || strings.ContainsAny(d.ID+d.Number, "/+#\x00") {
		return "", "", "", fmt.Errorf("invalid managed device identity")
	}
	prefix := "plugin/" + service + "/devices/"
	return prefix + "command/" + d.Number + "/+", prefix + "telemetry/control/" + d.Number, prefix + "telemetry/control/" + d.ID, nil
}

func (p *PlatformClient) startManagedSubscriptions() {
	p.managedOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		p.managedCancel = cancel
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				if err := p.refreshManagedSubscriptions(ctx); err != nil {
					p.logger.Warn("managed HTTP device subscriptions not ready")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (p *PlatformClient) refreshManagedSubscriptions(ctx context.Context) error {
	for page := 1; page <= 256; page++ {
		var response struct {
			Code int `json:"code"`
			Data struct {
				List  []managedDevice `json:"list"`
				Total int             `json:"total"`
			} `json:"data"`
		}
		if err := p.managedPost(ctx, "/api/v1/plugin/devices", map[string]interface{}{"service_identifier": "HTTP", "device_type": "1", "page": page, "page_size": 100}, &response); err != nil {
			return err
		}
		if response.Code != 200 {
			return fmt.Errorf("device scope unavailable")
		}
		for _, d := range response.Data.List {
			command, control, legacyControl, err := scopedTopics(p.Config.ServiceIdentifier, d)
			if err != nil {
				return err
			}
			// Reapply after reconnect and device changes; never subscribe to all devices.
			if err = p.sdkClient.MQTT().Subscribe(command, 1, p.handleCommandMessage); err != nil {
				return err
			}
			if err = p.sdkClient.MQTT().Subscribe(control, 1, p.handleControlMessage); err != nil {
				return err
			}
			if legacyControl != control {
				id, number := d.ID, d.Number
				if err = p.sdkClient.MQTT().Subscribe(legacyControl, 1, func(topic string, payload []byte) {
					p.handleControlMessage(strings.TrimSuffix(topic, id)+number, payload)
				}); err != nil {
					return err
				}
			}
		}
		if page*100 >= response.Data.Total || len(response.Data.List) == 0 {
			return nil
		}
	}
	return fmt.Errorf("HTTP device scope exceeds subscription limit")
}
