

### deploy and see nvidia-smi 
```shell
kubectl apply -f deployment/nvidia-test-pod.yaml
kubectl describe pod nvidia-test-pod
kubectl delete pod nvidia-test-pod
kubectl logs nvidia-test-pod --tail=-1
kubectl exec -it nvidia-test-pod -- sh
```

### cuda test
```shell
python -c "import torch; print(torch.__version__, torch.cuda.is_available())"

```