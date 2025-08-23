
### apply
```shell
kubectl apply -f manifests/daemonset.yaml

```


### delete & auto create daemonset

```shell
kubectl delete pod -l app=micro-device-plugin -n kube-system

kubectl logs -l app=micro-device-plugin -n kube-system  --tail=-1

```