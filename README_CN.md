[English Document](README.md)

# ThingsPanel HTTP 协议插件

用于 HTTP 设备接入 ThingsPanel。设备通过 HTTP 上报遥测，ThingsPanel 通过 MQTT 下发指令或控制，插件支持直连 HTTP 下行和设备长轮询两种模式。

## 端口职责

| 配置 | 默认端口 | 用途 |
| --- | --- | --- |
| `server.port` | `19090` | 设备侧接口：上报、poll、ack |
| `server.http_port` | `19091` | ThingsPanel 平台回调和健康检查 |
| 设备下行端口 | `8080` | 设备自身监听的直连下行端口，默认路径 `/api/v1/control` 和 `/api/v1/command` |

设备上报、长轮询、ack 都走 `19090`。平台回调走 `19091`。

## 启动

```powershell
go mod tidy
go run .\cmd
```

## 设备身份

设备自身使用 `device_number` 标识，例如 `D001`。平台内部的 `device_id` 由 ThingsPanel 分配，插件查询设备配置后才会拿到。设备上报数据和长轮询 URL 都应该使用 `device_number`，不要使用平台内部 `device_id`。

## 设备上报

```text
POST http://插件地址:19090/api/v1/uplink
```

```powershell
curl.exe -X POST http://127.0.0.1:19090/api/v1/uplink `
  -H "Content-Type: application/json" `
  -H "Access-Token: aaaabbbb" `
  -d "{\"device_number\":\"D001\",\"temp\":25.5,\"hum\":60.2,\"status\":\"active\"}"
```

## 下行模式

### 直连模式

适用于设备或中间件有公网 IP、网络可达的场景。

在设备凭证里填写可选 `downlinkHost` 后，插件会直连：

```text
POST http://downlinkHost:8080/api/v1/control
POST http://downlinkHost:8080/api/v1/command
```

`commandUrl` 默认 `/api/v1/command`，`controlUrl` 默认 `/api/v1/control`，端口默认 `8080`。

### 长轮询模式

适用于设备在 NAT 后面、插件无法主动访问设备的场景。

如果 `downlinkHost` 为空，下行消息会进入队列，等待设备主动 poll。若配置了直连模式但直连请求失败，插件也会把消息转入长轮询队列兜底。

```text
GET http://插件地址:19090/api/v1/devices/{device_number}/poll
```

poll 最长等待 30 秒。

没有待下发消息时返回：

```json
{
  "commands": []
}
```

有待下发 command 时返回：

```json
{
  "commands": [
    {
      "type": "command",
      "device_number": "D001",
      "message_id": "msg-001",
      "method": "set",
      "params": {}
    }
  ]
}
```

设备执行 poll 到的 command 后回传结果：

```text
POST http://插件地址:19090/api/v1/devices/{device_number}/commands/{message_id}/ack
```

```json
{
  "ok": true,
  "data": {}
}
```

设备 poll 和 ack 使用设备凭证里的 `accessToken` 鉴权，可通过 `X-Api-Key`、`Access-Token` 或 `Authorization: Bearer <token>` 传入。

## 表单配置

`FormType=CFG` 返回 `internal/form_json/form_config.json`：

- `commandUrl`：Command 下发路径，默认 `/api/v1/command`。
- `controlUrl`：Control 下发路径，默认 `/api/v1/control`。
- `port`：设备直连下行 HTTP 端口，默认 `8080`。

`FormType=VCR` 返回 `internal/form_json/form_voucher.json`：

- `accessToken`：设备凭证。
- `downlinkHost`：可选。公网可达设备填写 IP 或域名；NAT 设备留空，使用长轮询。

## 平台回调

```text
http://插件地址:19091
```

健康检查：

```powershell
curl.exe http://127.0.0.1:19091/healthz
```

## 虚拟设备测试

长轮询测试：

```powershell
go run .\cmd\virtual_device -server http://127.0.0.1:19090/api/v1/uplink -device D001 -token aaaabbbb
```

直连下发测试：

```powershell
go run .\cmd\virtual_device -server http://127.0.0.1:19090/api/v1/uplink -device D001 -token aaaabbbb -poll=false -listen :8080
```

直连模式下，设备凭证里的 `downlinkHost` 填 `127.0.0.1`，端口保持默认 `8080`。

## 常见问题

- **设备不存在**：确认 ThingsPanel 中已经创建设备，并且设备编号和上报/长轮询 URL 中的 `device_number` 一致。
- **SDK 日志显示 `deviceID` 为空**：设备上报路径是按 `device_number` 查询设备配置。SDK 日志只打印 `deviceID`，因此这里为空不代表插件用了 `device_id` 查询。
- **上报返回 401**：检查 `Access-Token` 或 `X-Api-Key` 是否等于设备凭证。
- **直连下发连接被拒绝**：设备没有监听 `downlinkHost:port/controlUrl` 或 `commandUrl`。
- **长轮询一直返回空队列**：检查平台是否向正确的 `device_number` 下发，并确认设备凭证中 `downlinkHost` 为空，或直连模式已经失败进入队列。
