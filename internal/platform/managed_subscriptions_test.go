package platform

import (
	"strings"
	"testing"
)

func TestManagedSubscriptionsUseDeviceNumber(t *testing.T) {
	command, control, legacy, err := scopedTopics("HTTP", managedDevice{ID: "device-id", Number: "D001"})
	if err != nil || command != "plugin/HTTP/devices/command/D001/+" || control != "plugin/HTTP/devices/telemetry/control/D001" || legacy != "plugin/HTTP/devices/telemetry/control/device-id" {
		t.Fatalf("wrong scope: %s %s %s %v", command, control, legacy, err)
	}
	if strings.Contains(command, "/+/+") {
		t.Fatal("cross-device subscription")
	}
	if _, _, _, err = scopedTopics("HTTP", managedDevice{ID: "id", Number: "+"}); err == nil {
		t.Fatal("wildcard device number must be rejected")
	}
}
