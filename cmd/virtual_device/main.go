package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// VirtualSensor 虚拟 HTTP 传感器设备。
type VirtualSensor struct {
	deviceNumber string
	serverURL    string
	accessToken  string
	httpClient   *http.Client
	temperature  float64
	humidity     float64
	status       string
	reportPeriod time.Duration
}

func NewVirtualSensor(deviceNumber, serverURL, accessToken string, reportPeriod time.Duration) *VirtualSensor {
	return &VirtualSensor{
		deviceNumber: deviceNumber,
		serverURL:    serverURL,
		accessToken:  accessToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		temperature:  20.0 + rand.Float64()*10.0,
		humidity:     40.0 + rand.Float64()*30.0,
		status:       "active",
		reportPeriod: reportPeriod,
	}
}

func (v *VirtualSensor) Start() {
	ticker := time.NewTicker(v.reportPeriod)
	defer ticker.Stop()

	log.Printf("设备 %s 开始运行，上报周期: %v", v.deviceNumber, v.reportPeriod)

	if err := v.sendData(); err != nil {
		log.Printf("设备 %s 首次发送数据失败: %v", v.deviceNumber, err)
	}

	for range ticker.C {
		if err := v.sendData(); err != nil {
			log.Printf("设备 %s 发送数据失败: %v", v.deviceNumber, err)
		}
	}
}

func (v *VirtualSensor) sendData() error {
	v.updateSensorValues()

	payload := map[string]interface{}{
		"device_number": v.deviceNumber,
		"temp":          round(v.temperature, 1),
		"hum":           round(v.humidity, 1),
		"status":        v.status,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化上报数据失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, v.serverURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建HTTP请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if v.accessToken != "" {
		req.Header.Set("Access-Token", v.accessToken)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP上报失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("HTTP状态异常: %s, body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	log.Printf("设备 %s 上报成功: %s", v.deviceNumber, string(body))
	return nil
}

func (v *VirtualSensor) updateSensorValues() {
	v.temperature += (rand.Float64() - 0.5)
	if v.temperature < -40.0 {
		v.temperature = -40.0
	} else if v.temperature > 85.0 {
		v.temperature = 85.0
	}

	v.humidity += (rand.Float64() - 0.5) * 4.0
	if v.humidity < 0.0 {
		v.humidity = 0.0
	} else if v.humidity > 100.0 {
		v.humidity = 100.0
	}
}

func round(v float64, precision int) float64 {
	base := 1.0
	for i := 0; i < precision; i++ {
		base *= 10
	}
	return float64(int(v*base+0.5)) / base
}

func main() {
	var (
		serverURL    = flag.String("server", "http://localhost:19090/api/v1/uplink", "设备上报HTTP地址")
		deviceNumber = flag.String("device", "D001", "设备编号(device_number)")
		accessToken  = flag.String("token", "aaaabbbb", "Access-Token")
		period       = flag.Duration("period", 10*time.Second, "数据上报周期")
		deviceCount  = flag.Int("count", 1, "虚拟设备数量")
	)
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	log.Printf("启动虚拟HTTP传感器设备")
	log.Printf("上报地址: %s", *serverURL)
	log.Printf("起始设备编号: %s", *deviceNumber)
	log.Printf("设备数量: %d", *deviceCount)
	log.Printf("上报周期: %v", *period)

	for i := 0; i < *deviceCount; i++ {
		currentDeviceNumber := nextDeviceNumber(*deviceNumber, i)
		sensor := NewVirtualSensor(currentDeviceNumber, *serverURL, *accessToken, *period)
		go sensor.Start()
		time.Sleep(time.Duration(i) * time.Second)
	}

	select {}
}

func nextDeviceNumber(start string, offset int) string {
	if offset == 0 {
		return start
	}

	prefixEnd := len(start)
	for prefixEnd > 0 && start[prefixEnd-1] >= '0' && start[prefixEnd-1] <= '9' {
		prefixEnd--
	}
	if prefixEnd == len(start) {
		return fmt.Sprintf("%s-%d", start, offset+1)
	}

	prefix := start[:prefixEnd]
	numberPart := start[prefixEnd:]
	var number int
	if _, err := fmt.Sscanf(numberPart, "%d", &number); err != nil {
		return fmt.Sprintf("%s-%d", start, offset+1)
	}
	width := len(numberPart)
	return fmt.Sprintf("%s%0*d", prefix, width, number+offset)
}
