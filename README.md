[Checking Chinese version](README_CN.md)

# ThingsPanel HTTP Protocol Plugin

This is the official standard **HTTP Protocol Access Plugin**, helping devices that do not support MQTT to send data to the ThingsPanel IoT platform via HTTP POST requests.

## Features

- **Standard HTTP Access**: Provides RESTful API to receive device telemetry data.
- **Token Authentication**: Supports device security authentication based on Access Token.
- **Custom Forms**: 
  - **Protocol Config**: No global configuration required (Empty).
  - **Connection Config**: Provides Access Token input field.
- **Health Check**: Provides `/health` endpoint for platform status monitoring.
- **Docker Compatibility**: Optimized instructions for containerized deployment environments.

## Quick Start

### 1. Build and Run

**Build**
```bash
go mod tidy
go build -o tp-plugin-http cmd/main.go
```

**Run**
Ensure `configs/config.yaml` exists and is configured correctly:
```bash
./tp-plugin-http
```

### 2. Plugin Configuration (configs/config.yaml)

```yaml
server:
  http_port: 8081         # Plugin listening port (Device uplink port)
  
platform:
  url: "http://127.0.0.1:9999"    # ThingsPanel Platform address (Modify for Docker environments)
  mqtt_broker: "tcp://127.0.0.1:1883"
  service_identifier: "HTTP" # Service Identifier
```

## Platform Integration Guide (Important)

### 1. Docker Network Configuration

If your ThingsPanel platform runs in a Docker container, while this plugin runs on the host machine:

- **HTTP Service Address**: 
  - This is the address the Platform calls to reach the Plugin.
  - Please use the Docker Gateway address: `http://172.17.0.1:8081`
  - **Do NOT** use `127.0.0.1`, as that refers to the container itself.

- **Access Address**:
  - This is the address Devices use to report data.
  - Please use the server's **Public IP** or **LAN IP**.
  - Example: `http://10.147.17.226:8081/api/v1/uplink`

### 2. Device Uplink Example

After configuring the plugin and device, you can use `curl` to simulate device data reporting:

```bash
# Format: POST /api/v1/uplink
# Header: Access-Token: <Access Token configured on the platform>

curl -X POST http://localhost:8081/api/v1/uplink \
  -H "Content-Type: application/json" \
  -H "Access-Token: YOUR_DEVICE_TOKEN" \
  -d '{
        "temp": 25.5,
        "hum": 60.2,
        "status": "active"
      }'
```

## Directory Structure

```text
.
├── cmd/main.go            # Program Entry
├── configs/config.yaml    # Configuration File
├── internal/
│   ├── bootstrap/         # Startup Initialization (HTTP Server defined here)
│   ├── handler/           # Business Logic (Uplink processing)
│   └── form_json/         # Form Definitions (Voucher/Config)
└── logs/                  # Runtime Logs
```

## Troubleshooting

- **Plugin not started**: Check if `http_port` is occupied (`netstat -tulpn | grep 8081`).
- **Data not reported**: Check `app.log` for errors, confirm if Access Token matches.
- **Platform Connection Failed**: Ensure `platform.url` in `config.yaml` is accessible from the host.
