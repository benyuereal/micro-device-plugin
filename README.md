# micro-device-plugin
this is a device plugin support multi gpu limit resource


# 构建二进制
make build

# 构建镜像
docker build -t your-registry/micro-device-plugin:v1 .

# 部署到K8s
kubectl apply -f manifests/daemonset.yaml