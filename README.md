# Micro GPU Device Plugin for Kubernetes

## 概述
这是一个支持多GPU资源限制的Kubernetes设备插件，特别优化了对NVIDIA MIG设备的支持。它能够在Kubernetes集群中自动发现、管理和分配GPU资源，包括完整的GPU设备和MIG分区。

## 核心特性
- ✅ 完整的GPU设备发现与管理
- ✅ NVIDIA MIG设备支持（自动分区与配置）
- ✅ 设备健康检查与监控
- ⛔️ CDI（Container Device Interface）支持
- ✅ 资源回收与自动清理机制

## 前提条件
### 1. Kubernetes 集群
- Kubernetes 1.20+ 版本
- kubectl 配置完成

### 2 Containerd 配置
在 `/etc/containerd/config.toml` 中添加：

```toml
      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia]
          runtime_type = "io.containerd.runc.v2"
          privileged_without_host_devices = false
          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia.options]
            BinaryName = "/usr/bin/nvidia-container-runtime"
```
### 3 runclass配置
```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: nvidia
handler: nvidia  # 指向 nvidia-container-runtime
```

### 4. **GPU MIG 设置**:
```yaml
### 启用mig
sudo nvidia-smi -mig 1
### 创建 MIG 设备 (例如 3g.20gb 配置)
sudo nvidia-smi mig -cgi 9 -C
```

## 🚀 快速开始

### 部署设备插件
```yaml
kubectl apply -f manifests/daemonset.yaml
```

### 验证部署
```shell
kubectl get pods -n kube-system -l app=micro-device-plugin

kubectl logs -n kube-system -l app=micro-device-plugin --tail=50
```

### 测试示例
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nvidia-test-pod
spec:
  runtimeClassName: nvidia
  restartPolicy: Never
  containers:
    - name: test-container
      image: nvcr.io/nvidia/pytorch:24.05-py3
      imagePullPolicy: IfNotPresent
      # 关键修改：启动无限循环命令
      command: ["/bin/sh", "-c"]
      args: ["while true; do sleep 3600; done"]  # 每小时唤醒一次的永久循环
      resources:
        limits:
          nvidia.com/microgpu: 1
```

### 部署测试应用：

```shell

kubectl apply -f deployment/nvidia-test-pod.yaml
kubectl describe pod nvidia-test-pod
kubectl logs nvidia-test-pod --tail=-1
kubectl exec -it nvidia-test-pod -- sh

```

## 📊 功能特性
- 支持 NVIDIA GPU 和 MIG 设备管理
- 自动健康检查和设备回收
- CDI 设备注入支持
- 拓扑感知调度优化
- 多实例 GPU 资源切分

## 🛠 构建与部署


```shell
docker build -t your-registry/micro-device-plugin:v1.0.0 .
docker push your-registry/micro-device-plugin:v1.0.0
```

## 部署到kubernetes

```shell
kubectl apply -f manifests/daemonset.yaml
```

## 🔧 配置选项
| 环境变量 | 默认值            | 描述 |
|---------|----------------|------|
| `ENABLE_MIG` | `true`         | 启用 MIG 管理 |
| `MIG_PROFILE` | `3g.20gb`      | MIG 切分配置 |
| `MIG_INSTANCE_COUNT` | `0`            | MIG 实例数量 (0=自动计算) |
| `SKIP_CONFIGURED` | `true`         | 跳过已配置的 MIG 设备 |
| `CDI_ENABLED` | `false`        | 启用 CDI 设备注入 |
| `CDI_PREFIX` | `micro.device` | CDI 设备前缀 |