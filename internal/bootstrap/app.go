// internal/bootstrap/app.go
package bootstrap

import (
	"context"
	"time"
	"tp-plugin/internal/config"
	"tp-plugin/internal/platform"
	"tp-plugin/internal/protocol"
	"tp-plugin/internal/protocol/plugins/examples"

	"github.com/sirupsen/logrus"
)

// AppContext 应用程序上下文，包含所有运行时资源
type AppContext struct {
	Config          *config.Config
	PlatformClient  *platform.PlatformClient
	ProtocolHandler *protocol.SingleProtocolHandler
	ctx             context.Context
	cancel          context.CancelFunc
	heartbeatTicker *time.Ticker // 心跳定时器
}

// Shutdown 关闭应用程序
func (app *AppContext) Shutdown() {
	if app.cancel != nil {
		app.cancel()
	}

	// 停止协议处理器
	if app.ProtocolHandler != nil {
		if err := app.ProtocolHandler.Stop(); err != nil {
			logrus.WithError(err).Error("停止协议处理器失败")
		}
	}

	if app.PlatformClient != nil {
		app.PlatformClient.Close()
	}

	// 停止心跳定时器
	if app.heartbeatTicker != nil {
		app.heartbeatTicker.Stop()
	}

	logrus.Info("应用资源已释放")
}

// StartApp 启动应用程序
func StartApp(configPath string) (*AppContext, error) {
	// 1. 加载配置
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	// 2. 初始化日志系统
	if err := InitLogger(&cfg.Log); err != nil {
		return nil, err
	}

	// 3. 创建应用上下文
	ctx, cancel := context.WithCancel(context.Background())
	app := &AppContext{
		Config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	// 4. 初始化平台客户端
	platformClient, err := InitPlatformClient(&cfg.Platform)
	if err != nil {
		app.Shutdown()
		return nil, err
	}
	app.PlatformClient = platformClient

	// 5. 启动心跳定时器 - 每分钟发送一次心跳
	app.heartbeatTicker = time.NewTicker(time.Second * time.Duration(cfg.Server.HeartbeatTimeout))
	go func() {
		// 启动时立即发送一次心跳
		if err := platformClient.SendHeartbeat(ctx, cfg.Platform.ServiceIdentifier); err != nil {
			logrus.WithError(err).Error("首次发送心跳失败")
		} else {
			logrus.Info("首次心跳发送成功")
		}

		// 定时发送心跳
		for {
			select {
			case <-app.heartbeatTicker.C:
				if err := platformClient.SendHeartbeat(ctx, cfg.Platform.ServiceIdentifier); err != nil {
					logrus.WithError(err).Error("发送心跳失败")
				} else {
					logrus.Debug("心跳发送成功")
				}
			case <-ctx.Done():
				logrus.Info("心跳上报已停止")
				return
			}
		}
	}()

	// 6. 初始化单协议处理器
	if err := initializeProtocol(app, cfg); err != nil {
		app.Shutdown()
		return nil, err
	}

	// 7. 启动HTTP服务 (ThingsPanel callbacks + device uplink)
	if err := StartHTTPServer(platformClient, cfg.Server.HTTPPort, cfg.Server.HTTPAPIKey, cfg.Server.AutoRegister); err != nil {
		app.Shutdown()
		return nil, err
	}

	logrus.Info("服务就绪")
	return app, nil
}

// initializeProtocol 初始化单协议处理器
func initializeProtocol(app *AppContext, cfg *config.Config) error {
	// Create HTTP Demo Handler
	protocolHandler := examples.NewHTTPDemoHandler(cfg.Server.Port)

	// Create Single Protocol Handler
	singleHandler := protocol.NewSingleProtocolHandler(
		protocolHandler,
		app.PlatformClient,
		logrus.StandardLogger(),
	)

	// Start Protocol
	if err := singleHandler.Start(); err != nil {
		return err
	}

	app.ProtocolHandler = singleHandler
	logrus.Infof("Protocol Handler Initialized - %s (v%s)", protocolHandler.Name(), protocolHandler.Version())
	return nil
}
