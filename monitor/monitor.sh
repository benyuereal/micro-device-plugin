#!/bin/bash

while true; do
  clear
  echo "===== $(date) ====="
  echo "MIG 设备列表:"
  nvidia-smi mig -lgi

  echo -e "\n设备 1 状态:"
  nvidia-smi -i MIG-GPU-0-1 -q | grep -A 5 "Utilization" | grep -v "Gpu"

  echo -e "\n设备 2 状态:"
  nvidia-smi -i MIG-GPU-0-2 -q | grep -A 5 "Utilization" | grep -v "Gpu"

  sleep 1
done