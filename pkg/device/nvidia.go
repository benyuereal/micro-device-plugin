package device

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

type NVIDIADevice struct {
	id      string
	healthy bool
}

func (d *NVIDIADevice) ID() string        { return d.id }
func (d *NVIDIADevice) IsHealthy() bool   { return d.healthy }
func (d *NVIDIADevice) GetVendor() string { return "nvidia" }
func (d *NVIDIADevice) GetPath() string   { return "/dev/nvidia" + d.id }

type NVIDIAManager struct {
	lastDiscovery time.Time
	devices       []GPUDevice
	discoverySync sync.Mutex
}

func (m *NVIDIAManager) DiscoverGPUs() ([]GPUDevice, error) {
	m.discoverySync.Lock()
	defer m.discoverySync.Unlock()

	// 如果最近已经发现过设备，则使用缓存
	if time.Since(m.lastDiscovery) < 5*time.Minute && m.devices != nil {
		return m.devices, nil
	}

	// 执行nvidia-smi发现设备
	out, err := exec.Command("nvidia-smi", "-L").Output()
	if err != nil {
		return nil, err
	}

	var devices []GPUDevice
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "GPU") {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) < 4 {
			continue
		}
		id := strings.TrimPrefix(strings.Trim(parts[1], ":"), "GPU")
		devices = append(devices, &NVIDIADevice{
			id:      id,
			healthy: true, // 初始状态设为健康，后续检查更新
		})
	}

	m.devices = devices
	m.lastDiscovery = time.Now()
	return devices, nil
}

func (m *NVIDIAManager) CheckHealth(deviceID string) bool {
	// 简单的健康检查：尝试与设备通信
	cmd := exec.Command("nvidia-smi", "-i", deviceID, "--query-gpu=health", "--format=csv,noheader")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	health := strings.TrimSpace(string(out))
	return health == "Healthy"
}
