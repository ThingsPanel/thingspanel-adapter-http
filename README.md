# 协议插件模板

这是一个用于开发ThingsPanel协议插件的框架模板，提供了完整的插件开发基础架构，可以帮助开发者快速构建自定义协议插件。

## 特性

- 内置日志系统，支持文件轮转
- MQTT客户端集成
- 设备管理和缓存机制
- 表单配置管理
- HTTP服务支持
- 优雅的错误处理
- 配置文件管理
- **设备独立日志** - 为每个设备创建独立的日志文件，方便问题定位

## 重要概念

在开发协议插件前，请务必了解设备标识符的概念区分：
- **设备编号 (device_number)** - 设备本身的唯一标识符，从设备数据中提取  
- **设备ID (device_id)** - 平台分配给设备的内部ID，用于平台内部标识

详细说明请参考：[设备标识符概念说明](docs/设备标识符概念说明.md)

## 快速开始

### 安装

1. 克隆仓库
   ```bash
   git clone https://github.com/your-org/protocol-plugin-template.git
   cd protocol-plugin-template
   ```

2. 安装依赖
   ```bash
   go mod tidy
   ```

### 运行

1. 确保配置文件 `configs/config.yaml` 已正确设置

2. 启动服务
   ```bash
   cd cmd
   go run main.go
   ```

   也可以指定配置文件路径
   ```bash
   go run main.go --config ../configs/custom-config.yaml
   ```

### 开发自定义协议插件

1. 修改 `configs/config.yaml` 中的配置以匹配您的环境
2. 根据需要实现自定义协议处理逻辑
3. 如果仅需要控制台日志而不想生成日志文件，可将 `enableFile` 设置为 `false`
## 重要提示 (Important)

### Docker 部署网络配置
如果您的 ThingsPanel 平台运行在 Docker 容器中，而插件运行在宿主机上，请在平台配置插件时注意以下事项：

1. **HTTP 服务地址 (Service Address)**:
   - 请使用 Docker 网关地址，通常为: `http://172.17.0.1:8081`
   - **不要**使用 `127.0.0.1` 或 `localhost` (这会指向容器内部)

2. **设备接入地址 (Access Address)**:
   - 设备上报数据时，请使用服务器的**公网IP**或**局域网IP**。
   - 例如: `http://10.147.17.226:8081/api/v1/uplink`

3. **表单配置说明 (Form Config)**:
   - **协议配置 (Protocol Config)**: 本插件无需额外的全局协议配置，该页面为空是正常的。
   - **连接信息 (Connection)**: 请在此页面输入设备的 **Access Token** 以进行鉴权。


## 目录结构

```text
.
├── cmd/                    # 主程序入口
│   └── main.go            # 主程序
├── configs/               # 配置文件目录
│   └── config.yaml        # 主配置文件
├── internal/              # 内部包
│   ├── bootstrap/        # 应用引导和初始化
│   │   ├── app.go        # 应用程序上下文
│   │   ├── config.go     # 配置加载
│   │   ├── http.go       # HTTP服务初始化
│   │   ├── logger.go     # 日志初始化
│   │   └── platform.go   # 平台客户端初始化
│   ├── config/           # 配置结构定义
│   ├── form_json/        # 表单JSON定义
│   ├── handler/          # HTTP处理器
│   ├── pkg/              # 通用包
│   │   └── logger/       # 日志包
│   └── platform/         # 平台交互
├── examples/              # 示例代码
├── logs/                  # 日志文件目录(运行时生成)
└── go.mod                # Go模块文件
```

## 核心组件说明

### 1. 应用引导 (internal/bootstrap)

- 负责应用程序初始化和启动流程
- 管理应用生命周期和资源
- 提供优雅的服务启动和关闭机制
- 协调各个组件的初始化顺序

### 2. 配置管理 (internal/config)

- 定义了插件所需的各种配置结构
- 支持服务器配置、平台配置和日志配置
- 使用YAML格式配置文件

### 3. HTTP处理器 (internal/handler)

- 处理各种HTTP请求
- 实现了表单配置、设备断开连接、通知等处理函数
- 支持自定义处理逻辑

### 4. 日志系统 (internal/pkg/logger)

- 基于logrus的日志系统
- 支持日志级别控制
- 支持日志文件轮转
- 支持控制台彩色输出
- 支持文件日志开关，可选择仅输出到控制台
- **设备独立日志** - 为每个设备创建独立的日志文件，方便问题定位

#### 设备独立日志功能

为了解决大量设备接入时问题定位困难的问题，系统提供了设备独立日志功能：

- **独立文件**: 每个设备生成独立的日志文件 `logs/devices/{设备ID}.log`
- **完整记录**: 记录设备的所有数据交互、状态变化、指令发送等
- **自动轮转**: 支持日志文件大小和时间基础的自动轮转
- **工具支持**: 提供 `examples/device_log_viewer.sh` 脚本便于查看和分析

详细说明请参考: [设备独立日志功能说明](docs/设备独立日志功能说明.md)

### 5. 平台客户端 (internal/platform)

- 管理与ThingsPanel平台的通信
- 提供设备管理和缓存机制
- 处理遥测数据发送
- 管理设备状态和心跳

## 规范

- 官方插件开发说明文档

<http://thingspanel.io/zh-Hans/docs/system-development/eveloping-plug-in/customProtocol>

## 配置文件说明

配置文件位于 `configs/config.yaml`，主要包含以下配置：

### 服务器配置 (server)

```yaml
server:
  port: 15001             # 协议插件服务端口
  http_port: 15002        # HTTP服务端口
  heartbeatTimeout: 60    # 心跳超时时间(秒)
```

### 平台配置 (platform)

```yaml
platform:
  url: "http://example.com"       # 平台API地址
  mqtt_broker: "mqtt://broker"    # MQTT服务器地址
  mqtt_username: "username"       # MQTT用户名
  mqtt_password: "password"       # MQTT密码
  service_identifier: "Template"  # 服务标识符
```

### 日志配置 (log)

```yaml
log:
  level: "debug"          # 日志级别: debug, info, warn, error
  filePath: "logs/app.log" # 日志文件路径
  enableFile: true        # 是否将日志输出到文件
  maxSize: 100            # 每个日志文件的最大大小(MB)
  maxBackups: 3           # 保留的旧日志文件的最大数量
  maxAge: 28              # 保留日志文件的最大天数
  compress: true          # 是否压缩旧日志文件
```

## 表单规范

表单 JSON 结构规范

该文档描述了用于生成前端表单的 JSON 结构的规范。它确定了必须和可选字段，以及它们的预期值。

1. 总体结构
表单由一个数组构成，每个数组元素都代表一个表单元素。表单元素可以是各种类型，如输入框或表格。

    ```text
    [
        { /* 表单元素1 */ },
        { /* 表单元素2 */ },
        // ...
    ]
    ```

2. 字段定义

| 字段名称    | 必选/可选               | 数据类型 | 描述                                                                           | 示例或备注                            |
| ----------- | ----------------------- | -------- | ------------------------------------------------------------------------------ | ------------------------------------- |
| dataKey     | 必填                    | 字符串   | 用于唯一标识表单元素的键。                                                     | "temp", "table1"                      |
| label       | 必填                    | 字符串   | 显示为表单元素标签的文本。                                                     | "读取策略(秒)", "属性列表"            |
| placeholder | 可选                    | 字符串   | 显示在表单元素中作为提示的文本。                                               | "请输入时间间隔，单位s"               |
| type        | 必填                    | 字符串   | 表单元素的类型。目前支持的类型有："input" 和 "table"。                         | "input", "table"                      |
| validate    | 可选                    | 对象     | 包含表单验证规则的对象。                                                       | 见 validate 字段的详细描述            |
| └─message   | 必填                    | 字符串   | 当验证失败时显示的错误消息。                                                   | "读取策略不能为空"                    |
| └─required  | 可选                    | 布尔值   | 指定字段是否是必填项。                                                         | true, false                           |
| └─rules     | 可选                    | 字符串   | 用于验证字段值的正则表达式规则。                                               | "/^\d{1,}$/" — 值必须是一个或多个数字 |
| └─type      | 可选                    | 字符串   | 用于指定验证的类型，例如，"number" 表示字段值应为数字。                        | "number"                              |
| array       | 只在 "table" 类型中可用 | 数组     | 包含表格列定义的数组。每一个列定义都是一个表单元素对象，它有相同的结构和属性。 | 见 array 字段的详细描述               |

注意：

1. `validate` 字段和其子字段（`message`, `required`, `rules`, `type`）是一个嵌套的结构，它们定义了表单元素的验证规则。
2. `array` 字段只适用于类型为 "table" 的表单元素，并包含一个嵌套的表单元素对象数组，用于定义表格的列。
3. 示例
查看附录
4. 开发注意事项
提供开发人员注意事项和最佳实践，包括但不限于:
●保证 dataKey 的唯一性。
●为每个字段提供合适的 placeholder 来指导用户输入。
●使用合适的正则表达式进行输入验证。

### 附录

示例

```text
[
    {
  "dataKey": "temp",
  "label": "读取策略(秒)",
  "placeholder": "请输入时间间隔，单位s",
  "type": "input",
  "validate": {
   "message": "读取策略不能为空",
   "required": true,
   "rules": "/^\\d{1,}$/",
   "type": "number"
  }
 },
 {
  "type": "table",
  "label": "属性列表",
       "dataKey": "table1",
  "array": [
   {
    "dataKey": "Interval",
    "label": "读取策略(秒)",
    "placeholder": "请输入时间间隔，单位s",
    "type": "input",
    "validate": {
     "message": "读取策略不能为空",
     "required": true,
     "rules": "/^\\d{1,}$/",
     "type": "number"
    }
   }
  ]
 }
]
```

表单填写后生成的数据样例

```json
{
 "attribute1":0,
 "attribute2": "",
 "table1": [
  {
   "attribute1":0,
      "attribute2": ""
  },
  {
   "attribute1":0,
        "attribute2": ""
  }
 ]
}
```
