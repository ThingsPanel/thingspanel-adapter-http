// internal/bootstrap/http.go
package bootstrap

import (
	"fmt"
	"net/http"
	"tp-plugin/internal/handler"
	"tp-plugin/internal/platform"

	"github.com/sirupsen/logrus"
)

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// StartHTTPServer 启动平台回调HTTP服务
func StartHTTPServer(platformClient *platform.PlatformClient, httpPort int) error {
	// ThingsPanel callbacks handler
	httpHandler := handler.NewHTTPHandler(platformClient, logrus.StandardLogger(), "", false)
	tpHandlers := httpHandler.RegisterHandlers()

	mux := http.NewServeMux()

	// Local Handlers
	handleHealth := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}

	// Custom Intercepts (Bypass SDK for these paths)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/healthz", handleHealth)

	// SDK Fallback
	mux.Handle("/", tpHandlers)

	go func() {
		addr := fmt.Sprintf(":%d", httpPort)
		logrus.Infof("启动平台HTTP服务 [%s] (WITH HEALTH ALIAS)", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			logrus.Errorf("HTTP服务启动失败: %v", err)
		}
	}()

	return nil
}
