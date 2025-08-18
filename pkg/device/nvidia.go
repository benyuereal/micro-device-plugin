package device

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"k8s.io/klog/v2"
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

// 获取nvidia-smi的路径 - 使用新的挂载点
func getNvidiaSmiPath() string {
	// 优先使用环境变量指定的路径
	if customPath := os.Getenv("NVIDIA_SMI_PATH"); customPath != "" {
		klog.V(4).Infof("Using custom NVIDIA-SMI path: %s", customPath)
		return customPath
	}
	// 默认使用新的挂载路径
	return "/host-driver/nvidia-smi"
}

// 确保命令使用正确的库路径
func runNvidiaSmiCommand(args ...string) ([]byte, error) {
	cmd := exec.Command(getNvidiaSmiPath(), args...)

	// 设置关键环境变量 - 优先使用容器内库
	cmd.Env = append(os.Environ(),
		"LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu:/host-lib",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	)

	klog.V(5).Infof("Executing NVIDIA-SMI command: %v", cmd.Args)

	// 执行并返回结果
	return cmd.CombinedOutput()
}

func (m *NVIDIAManager) DiscoverGPUs() ([]GPUDevice, error) {
	m.discoverySync.Lock()
	defer m.discoverySync.Unlock()

	// 使用缓存机制
	if time.Since(m.lastDiscovery) < 5*time.Minute && m.devices != nil {
		klog.V(4).Infof("Using cached NVIDIA devices (last discovery: %s)", m.lastDiscovery)
		return m.devices, nil
	}

	klog.Info("Discovering NVIDIA devices")

	// 使用新的命令执行方式
	out, err := runNvidiaSmiCommand("-L")
	if err != nil {
		klog.Errorf("Failed to discover NVIDIA GPUs: %v", err)
		return nil, err
	}

	klog.V(5).Infof("NVIDIA-SMI output:\n%s", string(out))

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
			healthy: true, // 初始假设健康
		})
	}

	klog.Infof("Discovered %d NVIDIA devices", len(devices))
	for _, d := range devices {
		klog.Infof("NVIDIA Device: ID=%s", d.ID())
	}

	m.devices = devices
	m.lastDiscovery = time.Now()
	return devices, nil
}

func (m *NVIDIAManager) CheckHealth(deviceID string) bool {
	klog.V(5).Infof("Checking health of NVIDIA device %s", deviceID)

	// 使用新的命令执行方式
	out, err := runNvidiaSmiCommand("-i", deviceID, "--query-gpu=health", "--format=csv,noheader")
	if err != nil {
		klog.Errorf("Failed to check health for NVIDIA device %s: %v", deviceID, err)
		return false
	}

	health := strings.TrimSpace(string(out))
	healthy := health == "Healthy"

	klog.V(4).Infof("NVIDIA device %s health status: %s", deviceID, health)
	return healthy
}
