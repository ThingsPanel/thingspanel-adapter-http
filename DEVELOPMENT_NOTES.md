# ThingsPanel 插件开发调试指南

本文档记录了在远程主机 (`10.147.17.226`) 上开发和调试 ThingsPanel HTTP 协议插件的关键信息和经验。

## 1. 调试环境信息

- **服务器 IP**: `10.147.17.226`
- **SSH 用户**: `root`
- **SSH 密码**: `zjh314`
- **项目路径**: `~/` (直接在 root home 目录下运行)
- **操作系统**: Debian (Proxmox LXC 容器)

## 2. 系统架构与网络拓扑

此环境是一个混合部署环境，理解网络流向至关重要：

```mermaid
graph TD
    User[浏览器/用户] -->|访问| PlatformUI[ThingsPanel UI (Port 8080)]
    Device[设备/模拟器] -->|上报数据| Plugin[HTTP 插件 (Port 8081)]
    
    subgraph Docker容器 [ThingsPanel Docker 环境]
        Platform[Backend Core] 
        PlatformUI
    end
    
    subgraph Host主机 [宿主机 10.147.17.226]
        Plugin
        DockerEngine[Docker Engine]
    end

    Platform -->|HTTP请求 (Service Address)| Plugin
    Plugin -->|GRPC/HTTP回调| Platform
```

### 关键网络配置

1. **Platform -> Plugin (服务地址)**:
   - 平台运行在 Docker 容器内，**无法**通过 `127.0.0.1` 访问宿主机。
   - 必须使用 Docker 网桥网关地址: `http://172.17.0.1:8081`
   - **配置位置**: ThingsPanel UI -> 应用管理 -> 接入协议 -> HTTP插件 -> HTTP服务地址

2. **Device -> Plugin (接入地址)**:
   - 设备从外部网络访问，必须使用宿主机 IP。
   - 地址: `http://10.147.17.226:8081/api/v1/uplink`
   - **配置位置**: 此地址显示在 ThingsPanel UI 的设备连接信息中，但实际连接需使用真实IP。

## 3. 部署与调试流程

### 编译 (Cross-Compile)
开发机通常为 macOS/Windows，目标机为 Linux amd64。
```bash
GOOS=linux GOARCH=amd64 go build -o tp-plugin-linux cmd/main.go
```

### 部署脚本 (expect 示例)
建议使用 `expect` 脚本自动处理 SSH 交互和进程重启。

**关键步骤**:
1. `ssh` 登录。
2. **清理旧进程**: 务必使用 `fuser -k -n tcp 8081` 或 `pkill` 确保端口释放。
   - *坑*: 有时 `nohup` 进程会变成僵尸进程或无法被普通 kill 杀死，导致新进程无法绑定端口。
3. `scp` 上传新二进制文件。
4. `nohup` 启动新进程。

### 常用调试命令 (在服务器上)

```bash
# 查看日志
tail -f app.log

# 检查端口占用
netstat -tulpn | grep 8081

# 强制杀掉占用 8081 端口的进程 (最有效)
fuser -k -n tcp 8081

# 本地验证健康检查 (模拟平台心跳)
curl -v http://localhost:8081/health

# 本地验证表单配置 (模拟平台获取表单)
curl -v "http://localhost:8081/api/v1/form/config?form_type=CFG&protocol_type=HTTP&device_type=DIRECT"
curl -v "http://localhost:8081/api/v1/form/config?form_type=VCR&protocol_type=HTTP&device_type=DIRECT"
```

## 4. 表单配置开发 (JSON)

插件通过 `internal/form_json/` 目录下的 JSON 文件定义 UI 表单。

- **`form_config.json` (CFG)**: 
  - 对应 "协议配置" (Protocol Config) 页签。
  - 对于 HTTP 这种无状态协议，通常留空 (`[]`)。
  - *之前的坑*: 错误地保留了 Temperature/Humidity 示例字段，导致 UI 混乱。

- **`form_voucher.json` (VCR)**:
  - 对应 "连接信息" (Connection) 页签。
  - 用于定义设备鉴权所需的字段。
  - 示例内容 (Access Token):
    ```json
    [
        {
            "dataKey": "accessToken",
            "label": "Access Token",
            "type": "input",
            "validate": { "required": true }
        }
    ]
    ```

## 5. OPC-UA 集成建议 (Next Steps)

如果您计划开发 OPC-UA 插件，请遵循以下经验：

1. **独立仓库/目录**: 建议参考本 HTTP 插件结构。
2. **表单设计**: OPV-UA 需要复杂的配置（Endpoint, NodeID 等）。
   - 使用 `form_service_voucher.json` 定义服务器连接信息 (Endpoint, User, Cert)。
   - 使用 `form_config.json` 定义具体的点位映射 (NodeID -> Attribute)。
3. **依赖管理**: 使用 `github.com/gopcua/opcua` 库。
4. **长连接管理**: OPC-UA 是有状态的长连接，需要在内存中维护 `Session`。注意处理断线重连。
