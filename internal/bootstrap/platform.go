package bootstrap

import (
	"fmt"
	"tp-plugin/internal/config"
	"tp-plugin/internal/downlink"
	"tp-plugin/internal/platform"

	"github.com/sirupsen/logrus"
)

func InitPlatformClient(cfg *config.PlatformConfig) (*platform.PlatformClient, *downlink.Processor, error) {
	logrus.WithFields(logrus.Fields{
		"cfg_URL":        cfg.URL,
		"cfg_MQTTBroker": cfg.MQTTBroker,
	}).Info("platform client config check")

	platformClient, err := platform.NewPlatformClient(platform.Config{
		BaseURL:           cfg.URL,
		MQTTBroker:        cfg.MQTTBroker,
		MQTTUsername:      cfg.MQTTUsername,
		MQTTPassword:      cfg.MQTTPassword,
		ServiceIdentifier: cfg.ServiceIdentifier,
		TemplateSecret:    cfg.TemplateSecret,
	}, logrus.StandardLogger())
	if err != nil {
		return nil, nil, fmt.Errorf("create platform client failed: %w", err)
	}

	downlinkProcessor := downlink.NewProcessor(platformClient, logrus.StandardLogger())
	platformClient.SetCommandProcessor(downlinkProcessor)
	platformClient.SetControlProcessor(downlinkProcessor)

	logrus.Info("platform client ready")
	return platformClient, downlinkProcessor, nil
}
