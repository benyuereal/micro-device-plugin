# 基础构建阶段
FROM golang:1.19-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o micro-device-plugin ./cmd

# 最终运行时镜像
FROM ubuntu:22.04

LABEL maintainer="benyuereal"

# 安装基础工具和通用依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    kmod \
    ca-certificates \
    # NVIDIA 工具包（确保有nvidia-smi）
    nvidia-utils-525 \
    strace \
    lsof \
    && rm -rf /var/lib/apt/lists/*

# 华为 NPU 工具包（在实际环境中需要替换为具体依赖）
# RUN if [ "$GPU_VENDOR" = "huawei" ]; then \
#        apt-get update && apt-get install -y huawei-npu-toolkit; \
#    fi

# 复制应用二进制文件
COPY --from=builder /app/micro-device-plugin /usr/bin/
RUN chmod +x /usr/bin/micro-device-plugin

# 验证文件存在性和依赖
RUN test -f /usr/bin/micro-device-plugin && echo "Binary exists" || exit 1
RUN ldd /usr/bin/micro-device-plugin || true

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s \
  CMD curl -f http://localhost:8080/health || exit 1

# 调试模式入口点
CMD ["sh", "-c", "strace -f /usr/bin/micro-device-plugin || sleep 3600"]