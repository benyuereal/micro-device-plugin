# micro-device-plugin
this is a device plugin support multi gpu limit resource


# 构建二进制
make build

# 构建镜像
docker build -t benyuereal/micro-device-plugin:v1 .

# 部署到K8s
kubectl apply -f manifests/daemonset.yaml


### 安装go环境
```shell
sudo apt update 
sudo apt install -y golang-1.20
```


### 依赖整理
```shell
 go mod tidy
 go build ./cmd
```


### 镜像管理
```shell
docker rmi binyue/micro-device-plugin:v1.0.9
docker build -t binyue/micro-device-plugin:v1.0.13 .

docker push binyue/micro-device-plugin:v1.0.13

### 使用代理构建
docker build \
  --build-arg HTTP_PROXY=http://10.0.168.12:7890 \
  --build-arg HTTPS_PROXY=http://10.0.168.12:7890 \
  -t binyue/micro-device-plugin:v1.0.13 .
  
  
  ### 使用代理构建
docker build \
  --build-arg HTTP_PROXY=http://192.168.10.151:7890 \
  --build-arg HTTPS_PROXY=http://192.168.10.151:7890 \
  -t binyue/micro-device-plugin:v1.0.13 .


### 导入到microk8s
docker save binyue/micro-device-plugin:v1.0.13 -o micro-device-plugin.tar
sudo  ctr image  import micro-device-plugin.tar
sudo  ctr images ls | grep micro-device-plugin

```

### 本地加载镜像
```shell
# 1. 切换到 Minikube 的 Docker 环境
eval $(minikube docker-env)

# 2. 现在所有 docker 命令都针对 Minikube 环境
docker build \
  --build-arg HTTP_PROXY=http://10.0.168.12:7890 \
  --build-arg HTTPS_PROXY=http://10.0.168.12:7890 \
  -t binyue/micro-device-plugin:v1.0.13 .

# 3. 验证
docker images | grep my-image

# 4. 退出 Minikube 环境（完成后）
eval $(minikube docker-env -u)
```



### pod 管理
```shell
kubectl get pod -n kube-system
kubectl describe pod -l app=micro-device-plugin -n kube-system
kubectl logs -f -l app=micro-device-plugin -n kube-system
kubectl logs  -l app=micro-device-plugin -n kube-system --tail=-1

kubectl delete pod -l app=micro-device-plugin -n kube-system
kubectl delete daemonset -l app=micro-device-plugin -n kube-system
kubectl logs -l app=micro-device-plugin -n kube-system  --tail=-1
```


### 登陆到容器里面
```shell

kubectl exec -it micro-device-plugin-wfwnc n -n kube-system -- sh
```


### 设备管理

#### 开启mig
```shell
sudo nvidia-smi -i 0 -mig 1

sudo nvidia-smi -mig 1
```

#### mig管理
```shell

# 安装 GPU Operator
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
helm install --wait gpu-operator nvidia/gpu-operator \
  --set migManager.enabled=true \
  --set mig.strategy=mixed \
  --set mig.default=all-3g.20gb
  
  
  
  
```

#### 镜像复制
```shell
docker save binyue/micro-device-plugin:v1.0.13 -o micro-plugin.tar
sudo ctr -n k8s.io   image import   micro-plugin.tar
sudo ctr -n k8s.io images   ls | grep pytorch
```


#### gpumig
```shell
sudo systemctl stop gdm
sudo nvidia-smi -i 0 -mig 1
sudo reboot
sudo nvidia-smi mig -cgi 9 -C
sudo nvidia-smi mig -cgi 9 -C

```


### micro k8s 安装
```shell
sudo snap install microk8s --classic
# 将当前用户加入 microk8s 组，避免每次使用 sudo
sudo usermod -a -G microk8s $USER
sudo chown -f -R $USER ~/.kube
# 退出当前终端重新登录，使组权限生效
microk8s status --wait-ready

alias kubectl='microk8s kubectl'

```


### 代理

```shell
./clash -f config.yaml &


sudo mkdir -p /etc/systemd/system/docker.service.d
sudo vim /etc/systemd/system/docker.service.d/http-proxy.conf

[Service]
Environment="HTTP_PROXY=http://127.0.0.1:7890"
Environment="HTTPS_PROXY=http://127.0.0.1:7890"
Environment="NO_PROXY=localhost,127.0.0.1,.docker.internal"

sudo systemctl daemon-reload
sudo systemctl restart docker


echo 'export http_proxy="http://127.0.0.1:7890"' >> ~/.bashrc
echo 'export https_proxy="http://127.0.0.1:7890"' >> ~/.bashrc
echo 'export all_proxy="socks5://127.0.0.1:7890"' >> ~/.bashrc
source ~/.bashrc
source ~/.bashrc


sudo tee /etc/profile.d/proxy.sh <<EOF
export http_proxy="http://127.0.0.1:7890"
export https_proxy="http://127.0.0.1:7890"
export ftp_proxy="http://127.0.0.1:7890"
export no_proxy="localhost,127.0.0.1,10.96.0.0/12,.minikube"
EOF

source /etc/profile.d/proxy.sh
sudo apt update


sudo tee /etc/apt/apt.conf.d/95proxies <<EOF
Acquire::http::Proxy "http://127.0.0.1:7890";
Acquire::https::Proxy "http://127.0.0.1:7890";
EOF
```


#### microk8s 拉取镜像
```shell
microk8s.ctr image pull nvcr.io/nvidia/pytorch:24.05-py3
```

#### 测试Mig设备
```shell
# 获取 MIG 设备 UUID
nvidia-smi -L | grep MIG | awk '{print $6}'

# 使用特定 MIG 设备运行测试
docker run --rm \
  --gpus '"device=MIG-xxxx-xxxx-xxxx-xxxx"' \
  nvidia/cuda:12.4.0-base-ubuntu22.04 nvidia-smi
  
  docker run --rm   --gpus '"device=0"'   nvidia/cuda:12.4.0-base-ubuntu22.04   bash -c "env|grep NVIDIA_VISIBLE_DEVICES"
  # 运行容器并执行测试命令
  
  docker run --rm --gpus all nvcr.io/nvidia/pytorch:24.05-py3 \
  bash -c "nvidia-smi && echo NVIDIA_VISIBLE_DEVICES"
  
  
  sudo ctr run --rm -t nvcr.io/nvidia/pytorch:24.05-py3 \
  bash -c "nvidia-smi && echo NVIDIA_VISIBLE_DEVICES && env"
  
   docker run --rm --gpus '"device=MIG-f6cea3a7-2313-5911-842e-639d313cb6c1"' nvcr.io/nvidia/pytorch:24.05-py3 \
  bash -c "nvidia-smi && echo NVIDIA_VISIBLE_DEVICES && env"
  
docker run --rm --gpus all nvcr.io/nvidia/pytorch:24.05-py3 \
  bash -c "nvidia-smi && nvcc --version && echo 'CUDA available:' && python -c 'import torch; print(torch.cuda.is_available())'"


### contianerd 测试镜像

sudo ctr run --rm --runtime io.containerd.runtime.v1.linux   --gpus 0   nvcr.io/nvidia/pytorch:24.05-py3 nvidia-test   nvidia-smi
```

#### k3s镜像拉取

```shell
sudo k3s crictl images
# 在宿主机上导出 Docker 镜像
docker save nvcr.io/nvidia/pytorch:24.05-py3 -o pytorch.tar

# 导入到 K3s containerd
sudo k3s ctr images import pytorch.tar
```


#### /etc/nvidia-container-runtime/config.toml
```shell
[nvidia-container-runtime]
resources = ["nvidia.com/gpu", "nvidia.com/microgpu"]
```

```shell
func (s *DevicePluginServer) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
    // ... [现有代码] ...

    // ================= 添加 NVIDIA 工具注入 =================
    // 挂载 NVIDIA CLI 工具
    containerResp.Mounts = append(containerResp.Mounts, &pluginapi.Mount{
        HostPath:      "/usr/bin/nvidia-container-cli",
        ContainerPath: "/usr/bin/nvidia-container-cli",
        ReadOnly:      true,
    })
    
    // 挂载容器运行时配置
    containerResp.Mounts = append(containerResp.Mounts, &pluginapi.Mount{
        HostPath:      "/etc/nvidia-container-runtime",
        ContainerPath: "/etc/nvidia-container-runtime",
        ReadOnly:      true,
    })
    
    // 设置环境变量触发注入
    envs["NVIDIA_VISIBLE_DEVICES"] = strings.Join(physicalIDs, ",")
    envs["NVIDIA_DRIVER_CAPABILITIES"] = "all"  // 关键修改：从 compute,utility 改为 all
    
    // ================= 添加 PATH 设置 =================
    envs["PATH"] = "/usr/local/nvidia/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    
    // ... [现有代码] ...
}
```


#### k8s安装启动
```shell
# 使用 kubeadm 初始化集群，指定 containerd 的 socket 路径
# Calico 默认 CIDR，若用其他 CNI 需调整
# 若未关闭 swap 需添加此参数
sudo -E kubeadm init \
 --control-plane-endpoint "10.0.168.12:6443" \
  --apiserver-advertise-address "10.0.168.12" \
  --pod-network-cidr=10.244.0.0/16 \
  --cri-socket=unix:///run/containerd/containerd.sock \
  --ignore-preflight-errors=Swap \
  --v=5
       
```


#### containerd引入包
```shell
# 1. 从 default 命名空间导出镜像
sudo ctr -n default images export nvidia-pytorch.tar nvcr.io/nvidia/pytorch:24.05-py3

# 2. 将镜像导入到 k8s.io 命名空间（Kubernetes 使用的命名空间）
sudo ctr -n k8s.io images import nvidia-pytorch.tar

# 3. 验证镜像已存在于 k8s.io 命名空间
sudo ctr -n k8s.io images ls | grep pytorch
# 或者使用 crictl
crictl images | grep pytorch
```