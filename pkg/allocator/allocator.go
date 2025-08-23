package allocator

import (
	"errors"
	"sync"

	"k8s.io/klog/v2"
)

// Allocator 设备资源分配器接口
type Allocator interface {
	Allocate(ids []string, podUID string) error // 增加podUID参数
	Deallocate(ids []string)
	GetAllocatedDevices() []string
	CleanupOrphanedDevices(map[string]bool)
	GetPodUID(deviceID string) string    // 修改为 string 参数
	GetAllocationMap() map[string]string // 新增方法
}

// SimpleAllocator 简单的内存分配器实现
type SimpleAllocator struct {
	mu          sync.RWMutex
	allocated   map[string]bool   // 已分配设备ID
	deviceToPod map[string]string // 新增：设备到 Pod 的映射
}

func NewSimpleAllocator() *SimpleAllocator {
	return &SimpleAllocator{
		allocated:   make(map[string]bool),
		deviceToPod: make(map[string]string),
	}
}

// Allocate 分配设备资源
func (a *SimpleAllocator) Allocate(ids []string, podUID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 首先检查所有设备是否可用
	for _, id := range ids {
		if _, exists := a.allocated[id]; exists {
			return ErrDeviceAlreadyAllocated
		}
	}

	// 然后分配设备
	for _, id := range ids {
		a.allocated[id] = true
		klog.Infof("Device allocated: %s", id)
	}

	for _, id := range ids {
		a.allocated[id] = true
		a.deviceToPod[id] = podUID // 记录设备到 Pod 的映射
		klog.Infof("Device allocated: %s to pod %s", id, podUID)
	}

	return nil
}

// 新增方法：获取设备对应的 Pod UID
func (a *SimpleAllocator) GetPodUID(deviceID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.deviceToPod[deviceID]
}

// Deallocate 释放设备资源
func (a *SimpleAllocator) Deallocate(ids []string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, id := range ids {
		if _, exists := a.allocated[id]; exists {
			delete(a.allocated, id)
			delete(a.deviceToPod, id) // 清理映射关系
			klog.Infof("Device deallocated: %s", id)
		}
	}
}

// GetAllocatedDevices 获取所有已分配设备
func (a *SimpleAllocator) GetAllocatedDevices() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	devices := make([]string, 0, len(a.allocated))
	for id := range a.allocated {
		devices = append(devices, id)
	}
	return devices
}
func (a *SimpleAllocator) CleanupOrphanedDevices(discoveredIDs map[string]bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for id := range a.allocated {
		if !discoveredIDs[id] {
			delete(a.allocated, id)
			klog.Warningf("Cleaned orphaned device: %s", id)
		}
	}
}

// GetAllocationMap 返回设备分配状态的副本
func (a *SimpleAllocator) GetAllocationMap() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// 返回深拷贝防止并发修改
	result := make(map[string]string)
	for k, v := range a.deviceToPod {
		result[k] = v
	}
	return result
}

// 错误定义
var (
	ErrDeviceAlreadyAllocated = errors.New("device already allocated")
)
