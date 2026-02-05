// internal/handler/handler.go
package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	formjson "tp-plugin/internal/form_json"
	"tp-plugin/internal/pkg/logger"
	"tp-plugin/internal/platform"

	"github.com/ThingsPanel/tp-protocol-sdk-go/handler"
	"github.com/sirupsen/logrus"
)

// logrusWriter 实现 io.Writer 接口用于适配logrus
type logrusWriter struct {
	logger *logrus.Logger
	prefix string
}

func (w *logrusWriter) Write(p []byte) (n int, err error) {
	// 移除标准库logger添加的时间和文件信息前缀，只保留消息内容
	msg := string(p)

	// 查找第一个 ]: 后面的内容作为实际消息
	if idx := strings.Index(msg, "]: "); idx >= 0 {
		msg = msg[idx+3:]
	}

	// 移除末尾换行符
	msg = strings.TrimSpace(msg)

	// 添加前缀并记录日志
	w.logger.Info(w.prefix + msg)
	return len(p), nil
}

// HTTPHandler HTTP服务处理器
type HTTPHandler struct {
	platform *platform.PlatformClient
	logger   *logrus.Logger
	stdlog   *log.Logger

	// HTTP uplink settings
	httpAPIKey   string
	autoRegister bool
}

// NewHTTPHandler 创建HTTP处理器
func NewHTTPHandler(platform *platform.PlatformClient, logger *logrus.Logger, httpAPIKey string, autoRegister bool) *HTTPHandler {
	// 创建适配器
	writer := &logrusWriter{
		logger: logger,
		prefix: "[HTTP] ",
	}

	// 不使用标准库的前缀，因为我们会在写入时添加
	stdlog := log.New(writer, "", 0)

	return &HTTPHandler{
		platform:     platform,
		logger:       logger,
		stdlog:       stdlog,
		httpAPIKey:   httpAPIKey,
		autoRegister: autoRegister,
	}
}

// RegisterHandlers 注册所有HTTP处理器
func (h *HTTPHandler) RegisterHandlers() *handler.Handler {
	// 创建处理器，使用标准库Logger
	hdl := handler.NewHandler(handler.HandlerConfig{
		Logger: h.stdlog,
	})

	// 设置表单配置处理函数
	hdl.SetFormConfigHandler(h.handleGetFormConfig)

	// 设置设备断开连接处理函数
	hdl.SetDeviceDisconnectHandler(h.handleDeviceDisconnect)

	// 设置通知处理函数
	hdl.SetNotificationHandler(h.handleNotification)

	// 设置获取设备列表处理函数
	hdl.SetGetDeviceListHandler(h.handleGetDeviceList)

	return hdl
}

// handleGetFormConfig 处理获取表单配置请求
func (h *HTTPHandler) handleGetFormConfig(req *handler.GetFormConfigRequest) (interface{}, error) {
	h.logger.WithFields(logrus.Fields{
		"protocol_type": req.ProtocolType,
		"device_type":   req.DeviceType,
		"form_type":     req.FormType,
	}).Info("收到获取表单配置请求")

	// 根据请求类型返回不同的配置表单
	switch req.FormType {
	case "CFG": // 设备配置表单
		return h.readFormConfigEmbed("form_config.json"), nil
	case "VCR": // 设备凭证表单
		return h.readFormConfigEmbed("form_voucher.json"), nil
	case "SVCR": // 服务接入点凭证表单
		return h.readFormConfigEmbed("form_service_voucher.json"), nil
	default:
		return nil, fmt.Errorf("不支持的表单类型: %s", req.FormType)
	}
}

func (h *HTTPHandler) readFormConfigEmbed(name string) interface{} {
	b, err := formjson.FS.ReadFile(name)
	if err != nil {
		h.logger.WithError(err).Warn("read form json failed")
		return nil
	}
	var info interface{}
	if err := json.Unmarshal(b, &info); err != nil {
		h.logger.WithError(err).Warn("decode form json failed")
		return nil
	}
	return info
}

// handleDeviceDisconnect 处理设备断开连接请求
func (h *HTTPHandler) handleDeviceDisconnect(req *handler.DeviceDisconnectRequest) error {
	h.logger.WithField("device_id", req.DeviceID).Info("收到设备断开连接请求")

	// 清理设备缓存
	// Note: 因为原缓存是按 device_number 存储的,这里要先查出设备信息
	device, err := h.platform.GetDeviceByID(req.DeviceID)
	if err == nil { // 如果能找到设备就清理缓存
		h.platform.ClearDeviceCache(device.DeviceNumber)
	}

	// 发送设备离线状态
	// err = h.platform.SendDeviceStatus(req.DeviceID, platform.DeviceStatusOffline)
	// if err != nil {
	// 	h.logger.WithError(err).Error("发送设备离线状态失败")
	// 	return err
	// }

	return nil
}

// handleNotification 处理通知请求
func (h *HTTPHandler) handleNotification(req *handler.NotificationRequest) error {
	h.logger.WithFields(logrus.Fields{
		"message_type": req.MessageType,
		"message":      req.Message,
	}).Info("收到通知请求")

	// 解析消息内容
	var msgData map[string]interface{}
	if err := json.Unmarshal([]byte(req.Message), &msgData); err != nil {
		h.logger.WithError(err).Error("解析通知消息失败")
		return err
	}

	// 处理不同类型的通知
	switch req.MessageType {
	case "1": // 服务配置修改
		h.logger.Info("处理服务配置修改通知")
		// TODO: 实现服务配置修改逻辑
	case "2": // 设备配置修改
		h.logger.Info("处理设备配置修改通知")
		// TODO: 实现设备配置修改逻辑
	default:
		h.logger.Warnf("未知的通知类型: %s", req.MessageType)
	}

	return nil
}

// handleGetDeviceList 处理获取设备列表请求
func (h *HTTPHandler) handleGetDeviceList(req *handler.GetDeviceListRequest) (*handler.DeviceListResponse, error) {
	h.logger.WithFields(logrus.Fields{
		"voucher":            req.Voucher,
		"service_identifier": req.ServiceIdentifier,
		"page":               req.Page,
		"page_size":          req.PageSize,
	}).Info("收到获取设备列表请求")

	// 解析req的Voucher到formjson.SVCRForm结构体
	var svcrForm formjson.SVCRForm
	if err := json.Unmarshal([]byte(req.Voucher), &svcrForm); err != nil {
		h.logger.WithError(err).Error("解析凭证失败")
		return nil, err
	}

	rsp := handler.DeviceListResponse{
		Code:    200,
		Message: "获取成功",
	}

	// 记录设备日志统计信息到主日志
	deviceLogStats := logger.GetDeviceLogStats()
	h.logger.WithField("device_log_stats", deviceLogStats).Debug("设备日志统计信息")

	return &rsp, nil
}
