[中文文档](README_CN.md)

# ThingsPanel HTTP Protocol Plugin

HTTP access plugin for ThingsPanel. Devices report telemetry through HTTP; the platform sends commands or control messages through MQTT; the plugin supports both direct HTTP downlink and device long polling.

## Ports

| Config | Default | Purpose |
| --- | --- | --- |
| `server.port` | `19090` | Device-facing API: uplink, poll, ack |
| `server.http_port` | `19091` | ThingsPanel callbacks and health checks |
| Device downlink port | `8080` | Device-owned HTTP server for direct downlink, default path `/api/v1/control` |

Do not send device uplink traffic to `19091`. Platform callbacks use `19091`; devices use `19090`.

## Run

```powershell
go mod tidy
go run .\cmd
```

Default config: `configs/config.yaml`

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

## Device Uplink

```text
POST http://plugin-host:19090/api/v1/uplink
```

Example:

```powershell
curl.exe -X POST http://127.0.0.1:19090/api/v1/uplink `
  -H "Content-Type: application/json" `
  -H "Access-Token: aaaabbbb" `
  -d "{\"device_number\":\"D001\",\"temp\":25.5,\"hum\":60.2,\"status\":\"active\"}"
```

## Downlink Modes

### Direct Mode

Use this when the device or middleware has a public/reachable HTTP endpoint.

Set `downlinkHost` in the device voucher. The plugin posts directly to:

```text
POST http://downlinkHost:8080/api/v1/control
```

Command path defaults to `/api/v1/command`; control path defaults to `/api/v1/control`; port defaults to `8080`.

### Long Polling Mode

Use this when the device is behind NAT or otherwise not reachable by the plugin.

```text
GET http://plugin-host:19090/api/v1/devices/{device_number}/poll
```

The poll request waits up to 30 seconds. If no pending message exists, the response is:

```json
{
  "commands": []
}
```

When messages exist:

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

After executing a command from poll, the device reports the result:

```text
POST http://plugin-host:19090/api/v1/devices/{device_number}/commands/{message_id}/ack
```

```json
{
  "ok": true,
  "data": {}
}
```

Authentication uses the device `accessToken` from the voucher. Send it as `X-Api-Key`, `Access-Token`, or `Authorization: Bearer <token>`.

## Forms

`FormType=CFG` returns `internal/form_json/form_config.json`:

- `commandUrl`: default `/api/v1/command`.
- `controlUrl`: default `/api/v1/control`.
- `port`: default `8080`.

`FormType=VCR` returns `internal/form_json/form_voucher.json`:

- `accessToken`: device credential.
- `downlinkHost`: optional direct downlink host. Leave empty for long polling mode.

## Platform Callback

```text
http://plugin-host:19091
```

Health check:

```powershell
curl.exe http://127.0.0.1:19091/healthz
```

## Virtual Device

The current virtual device only reports telemetry:

```powershell
go run .\cmd\virtual_device -server http://127.0.0.1:19090/api/v1/uplink -device D001 -token aaaabbbb
```

It does not listen on `/api/v1/control` and does not poll yet.

## Project Structure

```text
cmd/                  entrypoints and virtual device
configs/              configuration
internal/bootstrap/   startup and platform client wiring
internal/protocol/    device-facing HTTP server
internal/platform/    ThingsPanel API/MQTT client
internal/downlink/    direct HTTP downlink and long-poll queues
internal/handler/     platform callback handlers
internal/form_json/   frontend form definitions
```
