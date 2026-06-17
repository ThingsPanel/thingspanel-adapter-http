[English Document](README.md)

# ThingsPanel HTTP 协议插件

用于 HTTP 设备接入 ThingsPanel。设备通过 HTTP 上报遥测，平台通过 MQTT 下发指令或控制，插件支持直连下发和设备长轮询两种模式。

## 端口职责

| 配置 | 默认端口 | 用途 |
| --- | --- | --- |
| `server.port` | `19090` | 设备侧接口：上报、poll、ack |
| `server.http_port` | `19091` | ThingsPanel 平台回调和健康检查 |
| 设备下行端口 | `8080` | 设备自身监听的直连下行端口，默认路径 `/api/v1/control` |

设备上报、长轮询、ack 都走 `19090`。平台回调走 `19091`。

## 启动

```powershell
go mod tidy
go run .\cmd
```

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

在设备凭证里填写 `downlinkHost` 后，插件会优先直连：

```text
POST http://downlinkHost:8080/api/v1/control
```

`commandUrl` 默认 `/api/v1/command`，`controlUrl` 默认 `/api/v1/control`，端口默认 `8080`。

### 长轮询模式

适用于设备在 NAT 后面、插件无法主动访问设备的场景。

```text
GET http://插件地址:19090/api/v1/devices/{device_number}/poll
```

poll 最长等待 30 秒。没有待下发消息时返回：

```json
{
  "commands": []
}
```

有待下发消息时返回：

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
