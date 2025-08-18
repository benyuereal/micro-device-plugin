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
sudo apt install -y golang-1
```


### 依赖整理
```shell
 go mod tidy
 go build ./cmd
```


### 镜像管理
```shell
docker build -t binyue/micro-device-plugin:v3 .

docker push binyue/micro-device-plugin:v3



```

### pod 管理
```shell
kubectl get pod -n kube-system
kubectl describe pod -l app=micro-device-plugin -n kube-system
```

