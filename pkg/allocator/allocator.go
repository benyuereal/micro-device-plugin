package allocator

import (
	"errors"
	"sync"

	"k8s.io/klog/v2"
)

// Allocator 设备资源分配器接口
type Allocator interface {
	Allocate(ids []string) error
	Deallocate(ids []string)
	GetAllocatedDevices() []string
	CleanupOrphanedDevices(map[string]bool)
}

// SimpleAllocator 简单的内存分配器实现
type SimpleAllocator struct {
	mu        sync.RWMutex
	allocated map[string]bool // 已分配设备ID
}

func NewSimpleAllocator() *SimpleAllocator {
	return &SimpleAllocator{
		allocated: make(map[string]bool),
	}
}

// Allocate 分配设备资源
func (a *SimpleAllocator) Allocate(ids []string) error {
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

	return nil
}

// Deallocate 释放设备资源
func (a *SimpleAllocator) Deallocate(ids []string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, id := range ids {
		if _, exists := a.allocated[id]; exists {
			delete(a.allocated, id)
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

// 错误定义
var (
	ErrDeviceAlreadyAllocated = errors.New("device already allocated")
)
