// internal/bootstrap/config.go
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"tp-plugin/internal/config"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// LoadConfig 加载配置文件，支持环境变量覆盖
func LoadConfig(configPath string) (*config.Config, error) {
	// 设置配置文件
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// 设置环境变量前缀
	viper.SetEnvPrefix("P")
	// 使 Viper 能够读取环境变量
	viper.AutomaticEnv()
	// 将配置键中的点号替换为下划线，以匹配环境变量命名规范
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	for _, key := range []string{"platform.api_token", "platform.managed", "server.auto_register"} {
		if err := viper.BindEnv(key); err != nil {
			return nil, err
		}
	}

	// 读取配置文件（如果存在）
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("读取配置文件失败: %v", err)
		}
		// 配置文件不存在时继续，使用环境变量
	}

	var cfg config.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %v", err)
	}
	if cfg.Platform.Managed {
		if strings.TrimSpace(cfg.Platform.APIToken) == "" {
			return nil, fmt.Errorf("managed HTTP plugin requires a runtime identity")
		}
		if cfg.Server.AutoRegister {
			return nil, fmt.Errorf("managed HTTP plugin requires devices to be created in the platform")
		}
	}

	logrus.WithFields(logrus.Fields{
		"port":            cfg.Server.Port,
		"heartbeat":       cfg.Server.HeartbeatTimeout,
		"log_level":       cfg.Log.Level,
		"log_path":        cfg.Log.FilePath,
		"enable_file_log": cfg.Log.EnableFile,
		"mqtt_broker":     cfg.Platform.MQTTBroker,
	}).Info("配置加载成功")

	return &cfg, nil
}

// EnsureLogDir 确保日志目录存在
func EnsureLogDir(logPath string) error {
	dir := filepath.Dir(logPath)
	return os.MkdirAll(dir, 0755)
}
