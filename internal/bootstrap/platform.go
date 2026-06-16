// internal/bootstrap/platform.go
package bootstrap

import (
	"fmt"
	"tp-plugin/internal/config"
	"tp-plugin/internal/downlink"
	"tp-plugin/internal/platform"

	"github.com/sirupsen/logrus"
)

// InitPlatformClient 初始化平台客户端
func InitPlatformClient(cfg *config.PlatformConfig) (*platform.PlatformClient, error) {
	// 调试信息
	logrus.WithFields(logrus.Fields{
		"cfg_URL":        cfg.URL,
		"cfg_MQTTBroker": cfg.MQTTBroker,
	}).Info("平台客户端配置检查")

	// 简化日志，去掉"正在初始化"的冗余信息
	platformClient, err := platform.NewPlatformClient(platform.Config{
		BaseURL:           cfg.URL,
		MQTTBroker:        cfg.MQTTBroker,
		MQTTUsername:      cfg.MQTTUsername,
		MQTTPassword:      cfg.MQTTPassword,
		ServiceIdentifier: cfg.ServiceIdentifier,
		TemplateSecret:    cfg.TemplateSecret,
	}, logrus.StandardLogger())

	if err != nil {
		return nil, fmt.Errorf("创建平台客户端失败: %v", err)
	}

	downlinkProcessor := downlink.NewProcessor(platformClient, logrus.StandardLogger())
	platformClient.SetCommandProcessor(downlinkProcessor)
	platformClient.SetControlProcessor(downlinkProcessor)

	logrus.Info("平台客户端就绪")
	return platformClient, nil
}
