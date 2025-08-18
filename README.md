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
docker rmi binyue/micro-device-plugin:v1.0.5
docker build -t binyue/micro-device-plugin:v1.0.7 .

docker push binyue/micro-device-plugin:v1.0.7



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

kubectl exec -it micro-device-plugin-8s7th n -n kube-system -- sh
```


