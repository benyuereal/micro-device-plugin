

### build
```shell


git pull 

docker build   --build-arg HTTP_PROXY=http://10.0.168.58:7890   --build-arg HTTPS_PROXY=http://10.0.168.58:7890   -t binyue/micro-device-plugin:v1.0.13 .



```
### transfer to containerd

```shell

docker save binyue/micro-device-plugin:v1.0.13 -o micro-device-plugin.tar

sudo ctr -n k8s.io images import  micro-device-plugin.tar

sudo ctr -n k8s.io images ls |grep micro-device-plugin

```

