package device

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

type NVIDIADevice struct {
	id          string
	deviceIndex string // 系统设备索引
	physicalID  string // 物理GPU ID
	migEnabled  bool   // 是否为MIG设备
	profile     string // MIG配置类型
	healthy     bool
}

func (d *NVIDIADevice) ID() string         { return d.id }
func (d *NVIDIADevice) IsHealthy() bool    { return d.healthy }
func (d *NVIDIADevice) GetVendor() string  { return "nvidia" }
func (d *NVIDIADevice) GetPath() string    { return "/dev/nvidia" + d.deviceIndex }
func (d *NVIDIADevice) IsMIG() bool        { return d.migEnabled }
func (d *NVIDIADevice) PhysicalID() string { return d.physicalID }
func (d *NVIDIADevice) Profile() string    { return d.profile }

type NVIDIAManager struct {
	lastDiscovery time.Time
	devices       []GPUDevice
	deviceMap     map[string]*NVIDIADevice // 设备ID到设备对象的映射
	discoverySync sync.Mutex
	migManager    *MIGManager
}

// 初始化MIG管理器
func NewNVIDIAManager() *NVIDIAManager {
	return &NVIDIAManager{
		migManager: NewMIGManager(),
		deviceMap:  make(map[string]*NVIDIADevice),
	}
}

// 获取nvidia-smi的路径
func getNvidiaSmiPath() string {
	if customPath := os.Getenv("NVIDIA_SMI_PATH"); customPath != "" {
		klog.V(4).Infof("Using custom NVIDIA-SMI path: %s", customPath)
		return customPath
	}
	return "/host-driver/nvidia-smi"
}

// 确保命令使用正确的库路径
func runNvidiaSmiCommand(args ...string) ([]byte, error) {
	cmd := exec.Command(getNvidiaSmiPath(), args...)
	cmd.Env = append(os.Environ(),
		"LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu:/host-lib",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	)
	klog.V(5).Infof("Executing NVIDIA-SMI command: %v", cmd.Args)
	return cmd.CombinedOutput()
}

// 执行MIG管理命令
func runMIGCommand(args ...string) ([]byte, error) {
	cmd := exec.Command("nvidia-smi", args...)
	cmd.Env = append(os.Environ(),
		"LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu:/host-lib",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	)
	klog.V(5).Infof("Executing MIG command: %v", cmd.Args)
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

	// 重置设备映射
	m.deviceMap = make(map[string]*NVIDIADevice)
	var devices []GPUDevice

	// 步骤1: 获取所有GPU设备列表
	out, err := runNvidiaSmiCommand("--query-gpu=index,uuid,memory.total,mig.mode.current", "--format=csv,noheader")
	if err != nil {
		klog.Errorf("Failed to discover NVIDIA GPUs: %v", err)
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) < 4 {
			continue
		}

		gpuIndex := strings.TrimSpace(fields[0])
		gpuUUID := strings.TrimSpace(fields[1])
		migMode := strings.TrimSpace(fields[3])

		// 步骤2: 检查MIG模式
		if migMode == "Enabled" {
			// 获取MIG设备
			migDevices, err := m.discoverMIGDevices(gpuIndex)
			if err != nil {
				klog.Errorf("Failed to discover MIG devices for GPU %s: %v", gpuIndex, err)
				continue
			}
			devices = append(devices, migDevices...)
		} else {
			// 普通GPU设备
			device := &NVIDIADevice{
				id:          gpuUUID,
				deviceIndex: gpuIndex,
				physicalID:  gpuIndex,
				migEnabled:  false,
				healthy:     true,
			}
			devices = append(devices, device)
			m.deviceMap[gpuUUID] = device
		}
	}

	klog.Infof("Discovered %d NVIDIA devices", len(devices))
	for _, d := range devices {
		nvDevice := d.(*NVIDIADevice)
		klog.Infof("NVIDIA Device: ID=%s, Index=%s, MIG=%v, Profile=%s",
			nvDevice.ID(), nvDevice.deviceIndex, nvDevice.IsMIG(), nvDevice.Profile())
	}

	m.devices = devices
	m.lastDiscovery = time.Now()
	return devices, nil
}

// 发现MIG设备
func (m *NVIDIAManager) discoverMIGDevices(gpuIndex string) ([]GPUDevice, error) {
	var devices []GPUDevice

	// 查询GPU上的MIG设备
	out, err := runNvidiaSmiCommand("-i", gpuIndex, "--query-mig=index,gpu_instance_id,compute_instance_id,profile_name", "--format=csv,noheader")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) < 4 {
			continue
		}

		migIndex := strings.TrimSpace(fields[0])
		profile := strings.TrimSpace(fields[3])

		// 创建唯一的MIG设备ID
		deviceID := gpuIndex + "-" + migIndex

		device := &NVIDIADevice{
			id:          deviceID,
			deviceIndex: migIndex,
			physicalID:  gpuIndex,
			migEnabled:  true,
			profile:     profile,
			healthy:     true,
		}
		devices = append(devices, device)
		m.deviceMap[deviceID] = device
	}

	return devices, nil
}

// 健康检查
func (m *NVIDIAManager) CheckHealth(deviceID string) bool {
	klog.V(5).Infof("Checking health of NVIDIA device %s", deviceID)

	// 从设备映射中获取设备
	device, exists := m.deviceMap[deviceID]
	if !exists {
		klog.Warningf("Device %s not found in device map", deviceID)
		return false
	}

	// 对于MIG设备，检查其物理GPU的健康
	targetID := deviceID
	if device.IsMIG() {
		targetID = device.PhysicalID()
	}

	// 使用更通用的健康检查方式
	out, err := runNvidiaSmiCommand("-i", targetID, "--query-gpu=utilization.gpu", "--format=csv,noheader")
	if err != nil {
		klog.Errorf("Failed to check health for NVIDIA device %s: %v", targetID, err)
		return false
	}

	// 如果能够获取到GPU利用率数据，则认为设备健康
	utilization := strings.TrimSpace(string(out))
	if utilization != "" {
		klog.V(4).Infof("NVIDIA device %s is healthy (utilization: %s%%)", targetID, utilization)
		return true
	}

	return false
}

// MIG管理功能
func (m *NVIDIAManager) ConfigureMIG() {
	klog.Info("Configuring MIG devices")
	m.migManager.Configure()
}

// MIG管理器
type MIGManager struct {
	enabled        bool
	profile        string
	skipConfigured bool
}

func NewMIGManager() *MIGManager {
	enabled := os.Getenv("ENABLE_MIG") == "true"
	profile := os.Getenv("MIG_PROFILE")
	if profile == "" {
		profile = "3g.20gb" // 默认20GB切分策略
	}

	skipConfigured := os.Getenv("SKIP_CONFIGURED") == "true"

	return &MIGManager{
		enabled:        enabled,
		profile:        profile,
		skipConfigured: skipConfigured,
	}
}

func (m *MIGManager) Configure() {
	if !m.enabled {
		klog.Info("MIG configuration is disabled")
		return
	}

	klog.Infof("Starting MIG configuration with profile: %s", m.profile)

	// 1. 启用MIG模式
	if err := m.enableMIGMode(); err != nil {
		klog.Errorf("Failed to enable MIG mode: %v", err)
		return
	}

	// 2. 创建MIG设备
	if err := m.createMIGDevices(); err != nil {
		klog.Errorf("Failed to create MIG devices: %v", err)
	}
}

func (m *MIGManager) enableMIGMode() error {
	out, err := runMIGCommand("--enable-mig")
	if err != nil {
		return err
	}
	klog.V(4).Infof("MIG enable output: %s", string(out))
	return nil
}

func (m *MIGManager) createMIGDevices() error {
	// 获取GPU列表
	out, err := runNvidiaSmiCommand("--query-gpu=index", "--format=csv,noheader")
	if err != nil {
		return err
	}

	gpuIndexes := regexp.MustCompile(`\d+`).FindAllString(string(out), -1)
	for _, index := range gpuIndexes {
		// 检查是否已启用MIG
		out, err := runNvidiaSmiCommand("-i", index, "--query-gpu=mig.mode.current", "--format=csv,noheader")
		if err != nil {
			klog.Errorf("Failed to check MIG status for GPU %s: %v", index, err)
			continue
		}

		currentMode := strings.TrimSpace(string(out))
		if currentMode != "Enabled" {
			// 启用MIG模式
			if _, err := runMIGCommand("-i", index, "--enable-mig"); err != nil {
				klog.Errorf("Failed to enable MIG for GPU %s: %v", index, err)
				continue
			}
			klog.Infof("Enabled MIG mode for GPU %s", index)
		} else {
			klog.Infof("GPU %s already in MIG mode", index)
		}

		// 检查现有MIG设备
		count, err := m.getMIGDeviceCount(index)
		if err != nil {
			klog.Errorf("Failed to get MIG device count for GPU %s: %v", index, err)
			continue
		}

		// 如果已切分且配置跳过，则跳过创建
		if count > 0 && m.skipConfigured {
			klog.Infof("Skipping GPU %s (already has %d MIG devices)", index, count)
			continue
		}

		// 跳过已有配置的设备
		if count > 0 {
			klog.Infof("GPU %s already has %d MIG devices, skipping creation", index, count)
			continue
		}

		// 应用最优切分策略
		if _, err := runMIGCommand("-i", index, "--create-gpu-instance", m.profile); err != nil {
			klog.Errorf("Failed to create GPU instance (%s) on GPU %s: %v", m.profile, index, err)
		} else {
			klog.Infof("Created GPU instance (%s) on GPU %s", m.profile, index)
		}
	}

	return nil
}

// 获取当前MIG设备数量
func (m *MIGManager) getMIGDeviceCount(gpuIndex string) (int, error) {
	out, err := runNvidiaSmiCommand("-i", gpuIndex, "--query-mig=count", "--format=csv,noheader")
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return count, err
}
