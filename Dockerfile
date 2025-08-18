# 构建阶段
FROM golang:1.19-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o micro-device-plugin ./cmd

# 运行时镜像
FROM ubuntu:22.04
LABEL maintainer="benyuereal"

# 安装基础工具
RUN apt-get update && apt-get install -y \
    curl \
    kmod \
    libcuda1 \
    nvidia-utils-525 \
    huawei-npu-toolkit \
    && rm -rf /var/lib/apt/lists/*

# 复制程序二进制文件
COPY --from=builder /app/micro-device-plugin /usr/bin/

# 设置健康检查
HEALTHCHECK --interval=30s --timeout=10s \
  CMD curl -f http://localhost:8080/health || exit 1

# 启动命令
ENTRYPOINT ["/usr/bin/micro-device-plugin"]