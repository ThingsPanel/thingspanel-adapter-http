# ThingsPanel HTTP 协议插件

这是一个官方标准的 **HTTP协议接入插件**，用于帮助不支持 MQTT 的设备通过 HTTP POST 请求将数据发送至 ThingsPanel 物联网平台。

## 功能特性

- **标准 HTTP 接入**: 提供 RESTful API 接收设备遥测数据。
- **Token 鉴权**: 支持基于 Access Token 的设备安全认证。
- **自定义表单**: 
  - **协议配置**: 无需全局配置 (Empty).
  - **连接配置**: 提供 Access Token 输入框.
- **健康检查**: 提供 `/health` 端点供平台监控插件状态.
- **Docker 兼容**: 针对容器化部署环境进行了优化说明.

## 快速开始

### 1. 编译与运行

**编译**
```bash
go mod tidy
go build -o tp-plugin-http cmd/main.go
```

**运行**
确保 `configs/config.yaml` 存在并配置正确：
```bash
./tp-plugin-http
```

### 2. 插件配置 (configs/config.yaml)

```yaml
server:
  http_port: 8081         # 插件监听端口 (设备上报数据端口)
  
platform:
  url: "http://127.0.0.1:9999"    # ThingsPanel 平台地址 (注意 Docker 环境需修改)
  mqtt_broker: "tcp://127.0.0.1:1883"
  service_identifier: "HTTP" # 服务标识符
```

## 平台接入指南 (重要)

### 1. Docker 环境网络配置

如果您的 ThingsPanel 平台运行在 Docker 容器中，而本插件运行在宿主机上：

- **HTTP 服务地址 (Service Address)**: 
  - 这是平台调用插件的地址。
  - 请使用 Docker 网关地址: `http://172.17.0.1:8081`
  - **切勿**使用 `127.0.0.1`，因为这对容器来说是指向它自己。

- **设备接入地址 (Access Address)**:
  - 这是设备上报数据的地址。
  - 请使用服务器的 **公网IP** 或 **局域网IP**。
  - 例如: `http://10.147.17.226:8081/api/v1/uplink`

### 2. 设备上报示例

配置好插件和设备后，您可以使用 `curl` 模拟设备上报数据：

```bash
# 格式: POST /api/v1/uplink
# Header: Access-Token: <在平台上配置的Access Token>

curl -X POST http://localhost:8081/api/v1/uplink \
  -H "Content-Type: application/json" \
  -H "Access-Token: YOUR_DEVICE_TOKEN" \
  -d '{
        "temp": 25.5,
        "hum": 60.2,
        "status": "active"
      }'
```

## 目录结构

```text
.
├── cmd/main.go            # 程序入口
├── configs/config.yaml    # 配置文件
├── internal/
│   ├── bootstrap/         # 启动初始化 (HTTP Server定义在此)
│   ├── handler/           # 业务逻辑 (Uplink处理)
│   └── form_json/         # 表单定义 (Voucher/Config)
└── logs/                  # 运行日志
```

## 故障排查

- **插件未启动**: 检查 `http_port` 是否被占用 (`netstat -tulpn | grep 8081`).
- **数据未上报**: 检查 `app.log` 是否有报错，确认 Access Token 是否匹配.
- **平台连接失败**: 确保 `config.yaml` 中的 `platform.url` 是宿主机可访问的地址.
