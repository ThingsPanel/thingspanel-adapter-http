# # syntax=docker/dockerfile:1
FROM golang:alpine AS builder

RUN go version

# 设置工作目录
WORKDIR /build

# 安装基础工具
RUN apk add --no-cache git

# 设置 Go 环境变量
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.io"

# 首先只复制依赖文件
COPY go.mod go.sum ./

# 预下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN go build -o tp-plugin-http ./cmd/main.go

# 使用 alpine 作为运行时镜像
FROM alpine:latest

# 安装运行时依赖
RUN apk add --no-cache tzdata

# 设置时区
ENV TZ=Asia/Shanghai

# 设置工作目录
WORKDIR /app

# 创建配置目录
RUN mkdir -p /app/config

# 从构建阶段复制二进制文件和配置文件
COPY --from=builder /build/tp-plugin-http .
COPY --from=builder /build/configs/config.yaml /app/config/

# 设置默认环境变量
ENV P_SERVER_PORT=19090 \
    P_SERVER_HTTP_PORT=19091 \
    P_PLATFORM_URL=http://127.0.0.1:9999 \
    P_PLATFORM_MQTT_BROKER=127.0.0.1:1883 \
    P_PLATFORM_MQTT_QOS=0 \
    P_LOG_LEVEL=info

# 暴露端口
EXPOSE 19090 19091

# 设置可执行权限
RUN chmod +x tp-plugin-http

# 启动应用
ENTRYPOINT [ "./tp-plugin-http", "-c", "/app/config/config.yaml" ]