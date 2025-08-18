package device

import (
	"time"
)

type SimulatorManager struct {
	lastDiscovery time.Time
	devices       []GPUDevice
}

func (m *SimulatorManager) DiscoverGPUs() ([]GPUDevice, error) {
	return []GPUDevice{
		&SimulatorDevice{id: "0", healthy: true},
		&SimulatorDevice{id: "1", healthy: true},
		&SimulatorDevice{id: "2", healthy: true},
	}, nil
}

func (m *SimulatorManager) CheckHealth(deviceID string) bool {
	// 模拟 10% 的失败率
	return time.Now().UnixNano()%10 != 0
}
