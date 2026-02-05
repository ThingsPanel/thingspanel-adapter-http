// internal/bootstrap/http.go
package bootstrap

import (
	"fmt"
	"net/http"
	"tp-plugin/internal/handler"
	"tp-plugin/internal/platform"

	"github.com/sirupsen/logrus"
)

// StartHTTPServer 启动HTTP服务
func StartHTTPServer(platformClient *platform.PlatformClient, httpPort int, httpAPIKey string, autoRegister bool) error {
	// ThingsPanel callbacks handler
	httpHandler := handler.NewHTTPHandler(platformClient, logrus.StandardLogger(), httpAPIKey, autoRegister)
	tpHandlers := httpHandler.RegisterHandlers()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpHandler.HandleHealthz)
	mux.HandleFunc("/api/v1/uplink", httpHandler.HandleUplink)
	mux.Handle("/", tpHandlers)

	go func() {
		addr := fmt.Sprintf(":%d", httpPort)
		logrus.Infof("启动HTTP服务 [%s]", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			logrus.Errorf("HTTP服务启动失败: %v", err)
		}
	}()

	return nil
}
