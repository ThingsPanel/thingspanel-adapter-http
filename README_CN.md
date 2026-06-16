[English Document](README.md)

# ThingsPanel HTTP 协议插件

用于 HTTP 设备接入 ThingsPanel：设备通过 HTTP 上报遥测，平台通过 MQTT 下发控制，插件再通过 HTTP POST 推送给设备。

## 端口职责

| 配置 | 默认端口 | 用途 |
| --- | --- | --- |
| `server.port` | `19090` | 设备上报入口，只接收 `POST /api/v1/uplink` |
| `server.http_port` | `19091` | ThingsPanel 平台回调入口和健康检查 |
| 设备下行端口 | `8080` | 设备自身监听的控制接收端口，默认路径 `/api/v1/control` |

不要把设备上报发到 `19091`；也不要把平台下发控制发回插件的 `19090/19091`。下发控制的目标是设备自己的 HTTP 服务。

## 快速启动

```powershell
go mod tidy
go run .\cmd
```

默认配置文件：`configs/config.yaml`

```yaml
server:
  port: 19090
  http_port: 19091
  http_api_key: "aaaabbbb"

platform:
  url: "http://127.0.0.1:9999"
  mqtt_broker: "tcp://127.0.0.1:1883"
  service_identifier: "HTTP"
```

启动前确保 ThingsPanel API 和 MQTT 服务可访问。

## 设备上报

设备上报地址：

```text
POST http://插件地址:19090/api/v1/uplink
```

示例：

```powershell
curl.exe -X POST http://127.0.0.1:19090/api/v1/uplink `
  -H "Content-Type: application/json" `
  -H "Access-Token: aaaabbbb" `
  -d "{\"device_number\":\"D001\",\"temp\":25.5,\"hum\":60.2,\"status\":\"active\"}"
```

说明：
- `device_number` 是设备编号，例如 `D001`。
- 插件会记录设备最近一次上报来源 IP，后续平台控制下发会优先使用这个 IP。
- Windows 本机测试建议使用 `127.0.0.1`，避免 `localhost` 被解析成 IPv6 `[::1]`。

## 平台回调

平台调用插件地址：

```text
http://插件地址:19091
```

健康检查：

```powershell
curl.exe http://127.0.0.1:19091/healthz
```

Docker 中平台访问宿主机插件时，通常使用 Docker 网关地址，例如：

```text
http://172.17.0.1:19091
```

## 平台下发控制

平台通过 MQTT 发布控制消息：

```text
Topic: plugin/HTTP/devices/telemetry/control/D001
Payload: {"xx":11}
```

插件收到后会：

1. 按 `D001` 查询设备。
2. 优先使用该设备最近一次上报 IP。
3. 读取设备下行端口和路径，默认 `8080` + `/api/v1/control`。
4. 向设备发送：

```text
POST http://设备IP:8080/api/v1/control
```

请求体：

```json
{
  "tp_device_number": "D001",
  "values": {
    "xx": 11
  }
}
```

如果日志出现 `connectex: No connection could be made...`，说明设备没有在目标 IP/端口上启动控制接收服务。

## 表单配置

`FormType=CFG` 返回 `internal/form_json/form_config.json`：

- `host`：设备下行地址，可选。默认使用设备最近一次上报 IP。
- `commandUrl`：Command 下发路径，默认 `/api/v1/command`。
- `controlUrl`：Control 下发路径，默认 `/api/v1/control`。
- `port`：设备下行 HTTP 端口，默认 `8080`。

`FormType=VCR` 返回 `internal/form_json/form_voucher.json`：

- `accessToken`：设备凭证。

## 虚拟设备

当前虚拟设备只负责模拟上报：

```powershell
go run .\cmd\virtual_device -server http://127.0.0.1:19090/api/v1/uplink -device D001 -token aaaabbbb
```

它不会监听 `/api/v1/control`。如果要测试平台下发，需要真实设备或另起一个 HTTP Server 监听 `8080/api/v1/control`。

## 目录结构

```text
cmd/                  程序入口与虚拟设备
configs/              配置文件
internal/bootstrap/   启动、配置、平台客户端初始化
internal/protocol/    设备上报 HTTP 服务
internal/platform/    ThingsPanel API/MQTT 客户端
internal/downlink/    平台下发到设备的 HTTP 处理
internal/handler/     平台回调处理
internal/form_json/   前端表单定义
```

## 常见问题

- **MQTT 连接失败**：检查 `platform.mqtt_broker` 是否可访问。
- **设备上报 401**：检查 `Access-Token` 或 `X-Api-Key` 是否等于 `server.http_api_key`。
- **控制下发连接被拒绝**：设备没有监听 `设备IP:port/controlUrl`。
- **下发到了 `[::1]`**：本机上报用了 `localhost`，请改用 `127.0.0.1`。
