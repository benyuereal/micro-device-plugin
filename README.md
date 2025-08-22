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

docker push binyue/micro-device-plugin:v1.0.11

### 使用代理构建
docker build \
  --build-arg HTTP_PROXY=http://192.168.10.159:7890 \
  --build-arg HTTPS_PROXY=http://192.168.10.159:7890 \
  -t binyue/micro-device-plugin:v1.0.13 .

### 导入到microk8s
docker save binyue/micro-device-plugin:v1.0.13 -o micro-device-plugin.tar
sudo microk8s ctr image import micro-device-plugin.tar
sudo microk8s ctr images ls | grep micro-device-plugin

```

### 本地加载镜像
```shell
# 1. 切换到 Minikube 的 Docker 环境
eval $(minikube docker-env)

# 2. 现在所有 docker 命令都针对 Minikube 环境
docker build \
  --build-arg HTTP_PROXY=http://192.168.10.151:7890 \
  --build-arg HTTPS_PROXY=http://192.168.10.151:7890 \
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
sudo microk8s ctr image import 
sudo microk8s ctr images ls | grep pytorch
```
