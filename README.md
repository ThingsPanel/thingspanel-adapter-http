[中文文档](README_CN.md)

# ThingsPanel HTTP Protocol Plugin

HTTP access plugin for ThingsPanel. Devices report telemetry through HTTP; ThingsPanel sends commands or control messages through MQTT; the plugin supports both direct HTTP downlink and device long polling.

## Ports

| Config | Default | Purpose |
| --- | --- | --- |
| `server.port` | `19090` | Device-facing API: uplink, poll, ack |
| `server.http_port` | `19091` | ThingsPanel callbacks and health checks |
| Device downlink port | `8080` | Device-owned HTTP server for direct downlink, default paths `/api/v1/control` and `/api/v1/command` |

Device uplink, long polling, and ack requests use `19090`. Platform callbacks use `19091`.

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

## Device Identity

Devices identify themselves with `device_number`, for example `D001`. The platform's internal `device_id` is used by ThingsPanel after the plugin resolves the device configuration. Device payloads and long-poll URLs should use `device_number`, not `device_id`.

## Device Uplink

```text
POST http://plugin-host:19090/api/v1/uplink
```

```powershell
curl.exe -X POST http://127.0.0.1:19090/api/v1/uplink `
  -H "Content-Type: application/json" `
  -H "Access-Token: aaaabbbb" `
  -d "{\"device_number\":\"D001\",\"temp\":25.5,\"hum\":60.2,\"status\":\"active\"}"
```

## Downlink Modes

### Direct Mode

Use this when the device or middleware has a public/reachable HTTP endpoint.

Set optional `downlinkHost` in the device voucher. The plugin posts directly to:

```text
POST http://downlinkHost:8080/api/v1/control
POST http://downlinkHost:8080/api/v1/command
```

Command path defaults to `/api/v1/command`; control path defaults to `/api/v1/control`; port defaults to `8080`.

### Long Polling Mode

Use this when the device is behind NAT or otherwise not reachable by the plugin.

If `downlinkHost` is empty, downlink messages are queued for the device to fetch. If direct mode is configured but the direct request fails, the plugin also falls back to the long-poll queue.

```text
GET http://plugin-host:19090/api/v1/devices/{device_number}/poll
```

The poll request waits up to 30 seconds.

No pending message:

```json
{
  "commands": []
}
```

Pending command:

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

Long polling test:

```powershell
go run .\cmd\virtual_device -server http://127.0.0.1:19090/api/v1/uplink -device D001 -token aaaabbbb
```

Direct downlink test:

```powershell
go run .\cmd\virtual_device -server http://127.0.0.1:19090/api/v1/uplink -device D001 -token aaaabbbb -poll=false -listen :8080
```

For direct mode, set the device voucher `downlinkHost` to `127.0.0.1` and keep the default port `8080`.

## Troubleshooting

- **Device not found**: confirm the ThingsPanel device exists and its device number matches the `device_number` in payloads and URLs.
- **SDK log shows empty `deviceID` while querying**: the plugin queries by `device_number` for device uplink. The SDK log only prints `deviceID`, so an empty value there does not mean the plugin used `device_id`.
- **Uplink 401**: check `Access-Token` or `X-Api-Key` against the device credential.
- **Direct downlink connection refused**: the device is not listening on `downlinkHost:port/controlUrl` or `commandUrl`.
- **Long polling returns empty commands**: check that the platform sent the downlink to the expected `device_number` and that the device credential leaves `downlinkHost` empty or direct mode failed.

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
