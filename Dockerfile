# 第一阶段：使用官方golang镜像构建
FROM golang:1.19 AS builder

# 设置工作目录
WORKDIR /workspace
COPY . .

# 静态编译，确保零依赖
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o /usr/bin/micro-device-plugin ./cmd

# 第二阶段：使用Ubuntu基础镜像
FROM ubuntu:22.04

# 替换为阿里云源
RUN sed -i 's|http://archive.ubuntu.com/ubuntu|https://mirrors.aliyun.com/ubuntu|g' /etc/apt/sources.list && \
    sed -i 's|http://security.ubuntu.com/ubuntu|https://mirrors.aliyun.com/ubuntu|g' /etc/apt/sources.list


# 安装必要的运行依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    kmod \
    libunwind8 \
    && rm -rf /var/lib/apt/lists/*

# 设置环境变量（修复警告问题）
ENV LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu

# 从构建阶段复制静态编译的二进制文件
COPY --from=builder /usr/bin/micro-device-plugin /usr/bin/

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s \
    CMD curl -f http://localhost:8080/health || exit 1

# 容器入口点
ENTRYPOINT ["/usr/bin/micro-device-plugin"]