[中文文档](README_CN.md)

# ThingsPanel HTTP Protocol Plugin

HTTP access plugin for ThingsPanel. Devices report telemetry through HTTP; the platform sends control messages through MQTT; the plugin forwards control messages to devices through HTTP POST.

## Ports

| Config | Default | Purpose |
| --- | --- | --- |
| `server.port` | `19090` | Device uplink only: `POST /api/v1/uplink` |
| `server.http_port` | `19091` | ThingsPanel callbacks and health checks |
| Device downlink port | `8080` | Device-owned HTTP server for control, default path `/api/v1/control` |

Do not send device uplink traffic to `19091`. Do not send control traffic back to plugin ports `19090/19091`; control is posted to the device HTTP server.

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

Make sure the ThingsPanel API and MQTT broker are reachable.

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

The plugin stores the latest source IP for each `device_number` and uses it for later control downlink. On Windows, prefer `127.0.0.1` over `localhost` in local tests to avoid IPv6 `[::1]`.

## Platform Callback

```text
http://plugin-host:19091
```

Health check:

```powershell
curl.exe http://127.0.0.1:19091/healthz
```

For Docker platform-to-host access, use the Docker gateway address, for example:

```text
http://172.17.0.1:19091
```

## Control Downlink

Platform MQTT message:

```text
Topic: plugin/HTTP/devices/telemetry/control/D001
Payload: {"xx":11}
```

The plugin then posts to the device:

```text
POST http://device-ip:8080/api/v1/control
```

Body:

```json
{
  "tp_device_number": "D001",
  "values": {
    "xx": 11
  }
}
```

If the log says connection refused, the device is not listening on the target IP/port/path.

## Forms

`FormType=CFG` returns `internal/form_json/form_config.json`:

- `host`: optional device downlink host. The latest uplink IP is used first.
- `commandUrl`: default `/api/v1/command`.
- `controlUrl`: default `/api/v1/control`.
- `port`: default `8080`.

`FormType=VCR` returns `internal/form_json/form_voucher.json`:

- `accessToken`: device credential.

## Virtual Device

The current virtual device only reports telemetry:

```powershell
go run .\cmd\virtual_device -server http://127.0.0.1:19090/api/v1/uplink -device D001 -token aaaabbbb
```

It does not listen on `/api/v1/control`. To test downlink, use a real device or start a separate HTTP server on `8080/api/v1/control`.

## Project Structure

```text
cmd/                  entrypoints and virtual device
configs/              configuration
internal/bootstrap/   startup and platform client wiring
internal/protocol/    device uplink HTTP server
internal/platform/    ThingsPanel API/MQTT client
internal/downlink/    HTTP downlink to devices
internal/handler/     platform callback handlers
internal/form_json/   frontend form definitions
```

## Troubleshooting

- **MQTT connection failed**: check `platform.mqtt_broker`.
- **Uplink 401**: check `Access-Token` or `X-Api-Key` against `server.http_api_key`.
- **Downlink connection refused**: the device is not listening on `device-ip:port/controlUrl`.
- **Downlink goes to `[::1]`**: local uplink used `localhost`; use `127.0.0.1`.
