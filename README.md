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
docker build -t binyue/micro-device-plugin:v1.0.11 .

docker push binyue/micro-device-plugin:v1.0.11

### 使用代理构建
docker build \
  --build-arg HTTP_PROXY=http://192.168.10.159:7890 \
  --build-arg HTTPS_PROXY=http://192.168.10.159:7890 \
  -t binyue/micro-device-plugin:v1.0.13


```

### 本地加载镜像
#### a主机
```shell
docker save -o micro-device-plugin.tar "binyue/micro-device-plugin:v1.0.11"
scp -P 22159 micro-device-plugin.tar k8sadmin@175.155.64.171:/home/k8sadmin

```

#### b主机

```shell
minikube cp micro-device-plugin.tar /home/docker/micro-device.tar
minikube ssh -- docker load -i /home/docker/micro-device.tar
```

### pod 管理
```shell
kubectl get pod -n kube-system
kubectl describe pod -l app=micro-device-plugin -n kube-system
kubectl logs -f -l app=micro-device-plugin -n kube-system
kubectl delete pod -l app=micro-device-plugin -n kube-system
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

