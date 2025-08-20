package device

import (
	"fmt"
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
	klog.Infof("Executing NVIDIA-SMI command: %v", cmd.Args)
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

	// 查询GPU实例（GPU Instances）
	out, err := runNvidiaSmiCommand("mig", "-lgi", "-i", gpuIndex)
	output := strings.TrimSpace(string(out))

	// 处理无GPU实例的情况
	if strings.Contains(output, "No GPU instances found") {
		klog.Infof("No MIG GPU instances found on GPU %s", gpuIndex)
		return devices, nil
	}

	if err != nil {
		klog.Errorf("Failed to query GPU instances for GPU %s: %v", gpuIndex, err)
		return nil, err
	}

	lines := strings.Split(output, "\n")
	// 跳过表头（如果有）
	startIndex := 0
	for i, line := range lines {
		if strings.Contains(line, "GPU Instance ID") {
			startIndex = i + 1
			break
		}
	}

	// 如果没有找到表头，则从0开始
	if startIndex >= len(lines) {
		startIndex = 0
	}

	for i := startIndex; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// 解析GPU实例信息
		fields := strings.Fields(line)
		if len(fields) < 4 {
			klog.V(5).Infof("Skipping invalid GPU instance line: %s", line)
			continue
		}

		gpuInstanceID := fields[0]
		profileID := fields[1]

		// 获取计算实例（Compute Instances）
		ciOut, err := runNvidiaSmiCommand("mig", "-lci", "-i", gpuIndex, "-gi", gpuInstanceID)
		if err != nil {
			klog.Errorf("Failed to query compute instances for GI %s: %v", gpuInstanceID, err)
			continue
		}

		ciOutput := strings.TrimSpace(string(ciOut))
		// 处理无计算实例的情况
		if strings.Contains(ciOutput, "No compute instances found") {
			klog.V(4).Infof("No compute instances found for GPU instance %s on GPU %s", gpuInstanceID, gpuIndex)
			continue
		}

		ciLines := strings.Split(ciOutput, "\n")
		ciStartIndex := 0
		for j, ciLine := range ciLines {
			if strings.Contains(ciLine, "Compute Instance ID") {
				ciStartIndex = j + 1
				break
			}
		}

		for j := ciStartIndex; j < len(ciLines); j++ {
			ciLine := strings.TrimSpace(ciLines[j])
			if ciLine == "" {
				continue
			}

			ciFields := strings.Fields(ciLine)
			if len(ciFields) < 3 { // 至少需要ID, Placement, State
				klog.V(5).Infof("Skipping invalid compute instance line: %s", ciLine)
				continue
			}

			computeInstanceID := ciFields[0]

			// 获取profile名称（将ID转换为可读名称）
			profileName, err := m.getProfileName(profileID)
			if err != nil {
				klog.Warningf("Failed to get profile name for ID %s: %v", profileID, err)
				profileName = "unknown"
			}

			// 创建设备ID: GPUIndex-GI-CI
			deviceID := fmt.Sprintf("%s-GI%s-CI%s", gpuIndex, gpuInstanceID, computeInstanceID)

			device := &NVIDIADevice{
				id:          deviceID,
				deviceIndex: gpuInstanceID, // 使用GPU实例ID作为设备索引
				physicalID:  gpuIndex,
				migEnabled:  true,
				profile:     profileName,
				healthy:     true,
			}
			devices = append(devices, device)
			m.deviceMap[deviceID] = device
		}
	}

	return devices, nil
}

func (m *NVIDIAManager) getProfileName(profileID string) (string, error) {
	// 查询所有可用profile
	out, err := runNvidiaSmiCommand("mig", "-lgip")
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if strings.Contains(line, profileID) {
			// 示例行: "   19     4     4      0       1      0     0     0     0     1     0     0      0      0     0     0     0     0     0     0     0     0     0     0     0     0     0     0     0     0     0     0  1g.10gb"
			fields := strings.Fields(line)
			if len(fields) > 0 {
				// 最后一个字段是profile名称
				return fields[len(fields)-1], nil
			}
		}
	}
	return "unknown", fmt.Errorf("profile not found for ID %s", profileID)
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
	instanceCount  int    // 每个GPU上要创建的实例数
	gpuMemory      uint64 // GPU显存大小(MB)
}

func NewMIGManager() *MIGManager {
	enabled := os.Getenv("ENABLE_MIG") == "true"
	profile := os.Getenv("MIG_PROFILE")
	if profile == "" {
		profile = "3g.20gb" // 默认20GB切分策略
	}

	skipConfigured := os.Getenv("SKIP_CONFIGURED") == "true"

	// 读取实例数量配置
	instanceCount := 0 // 0表示自动计算
	if countStr := os.Getenv("MIG_INSTANCE_COUNT"); countStr != "" {
		if count, err := strconv.Atoi(countStr); err == nil {
			instanceCount = count
		}
	}

	return &MIGManager{
		enabled:        enabled,
		profile:        profile,
		skipConfigured: skipConfigured,
		instanceCount:  instanceCount,
	}
}

func (m *MIGManager) Configure() {

	klog.Info("MIG configuration is in process ")

	if !m.enabled {
		klog.Info("MIG configuration is disabled")
		return
	}

	klog.Infof("Starting MIG configuration with profile: %s", m.profile)

	// 检查设备是否支持MIG
	if supported, err := m.isMigSupported(); err != nil {
		klog.Errorf("Failed to check MIG support: %v", err)
		return
	} else if !supported {
		klog.Warning("MIG is not supported on this device. Skipping MIG configuration.")
		return
	}

	// 2. 创建MIG设备
	if err := m.createMIGDevices(); err != nil {
		klog.Errorf("Failed to create MIG devices: %v", err)
	}
}

// 检查设备是否支持MIG
func (m *MIGManager) isMigSupported() (bool, error) {
	// 检查MIG支持状态
	out, err := runNvidiaSmiCommand("mig", "-lgip")
	output := strings.TrimSpace(string(out))

	// 先检查特定不支持信息
	if strings.Contains(output, "No MIG-supported devices found") {
		klog.V(4).Info("MIG not supported: No MIG-supported devices found")
		return false, nil
	}

	// 检查其他不支持情况
	if strings.Contains(output, "not supported") {
		klog.V(4).Infof("MIG not supported: %s", output)
		return false, nil
	}

	// 处理命令错误
	if err != nil {
		klog.V(4).Infof("MIG command failed: %s", output)
		return false, fmt.Errorf("MIG command failed: %v", err)
	}

	// 检查有效输出（应该包含设备信息）
	if len(output) > 0 && !strings.Contains(output, "error") {
		klog.V(4).Infof("MIG supported devices found: %s", output)
		return true, nil
	}

	klog.V(4).Infof("Unknown MIG support status: %s", output)
	return false, nil
}

func (m *MIGManager) enableMIGMode() error {
	out, err := runNvidiaSmiCommand("--enable-mig")
	if err != nil {
		return err
	}
	klog.V(4).Infof("MIG enable output: %s", string(out))
	return nil
}

// 获取GPU显存大小
func (m *MIGManager) getGPUMemory(gpuIndex string) (uint64, error) {
	out, err := runNvidiaSmiCommand("-i", gpuIndex, "--query-gpu=memory.total", "--format=csv,noheader,nounits")
	if err != nil {
		return 0, err
	}

	memoryStr := strings.TrimSpace(string(out))
	memoryMB, err := strconv.ParseUint(memoryStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU memory: %v", err)
	}

	return memoryMB, nil
}

// 从profile中提取显存需求 (GB)
func (m *MIGManager) getProfileMemoryReq() uint64 {
	parts := strings.Split(m.profile, ".")
	if len(parts) < 2 {
		return 0
	}

	memPart := parts[1]
	if strings.HasSuffix(memPart, "gb") {
		memPart = strings.TrimSuffix(memPart, "gb")
	} else if strings.HasSuffix(memPart, "g") {
		memPart = strings.TrimSuffix(memPart, "g")
	}

	memGB, err := strconv.ParseUint(memPart, 10, 64)
	if err != nil {
		klog.Warningf("Failed to parse memory requirement from profile %s: %v", m.profile, err)
		return 0
	}

	return memGB * 1024 // 转换为MB
}

/*
*
https://docs.nvidia.com/datacenter/tesla/mig-user-guide/index.html
*/
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
			if _, err := runNvidiaSmiCommand("-i", index, "--enable-mig"); err != nil {
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

		// 如果已有设备且不跳过，先销毁现有设备
		if count > 0 {
			klog.Infof("Destroying existing MIG devices on GPU %s", index)
			if _, err := runNvidiaSmiCommand("mig", "-i", index, "-dci"); err != nil {
				klog.Errorf("Failed to destroy compute instances on GPU %s: %v", index, err)
			}
			if _, err := runNvidiaSmiCommand("mig", "-i", index, "-dgi"); err != nil {
				klog.Errorf("Failed to destroy GPU instances on GPU %s: %v", index, err)
			}
			time.Sleep(2 * time.Second) // 等待资源释放
		}

		// 获取GPU显存大小
		totalMemory, err := m.getGPUMemory(index)
		if err != nil {
			klog.Errorf("Failed to get GPU memory for %s: %v", index, err)
			continue
		}

		// 计算最大可创建实例数
		profileMem := m.getProfileMemoryReq()
		maxInstances := 0

		if profileMem > 0 {
			maxInstances = int(totalMemory / profileMem)
			if maxInstances == 0 {
				klog.Warningf("GPU %s has insufficient memory (%dMB) for profile %s (%dMB required)",
					index, totalMemory, m.profile, profileMem)
				continue
			}
		}

		// 确定要创建的实例数量
		createCount := maxInstances
		if m.instanceCount > 0 {
			if m.instanceCount > maxInstances {
				klog.Warningf("Requested %d instances exceeds maximum %d for GPU %s",
					m.instanceCount, maxInstances, index)
				createCount = maxInstances
			} else {
				createCount = m.instanceCount
			}
		}

		if createCount == 0 {
			klog.Errorf("Cannot determine instance count for GPU %s", index)
			continue
		}

		klog.Infof("Creating %d MIG device(s) with profile %s on GPU %s", createCount, m.profile, index)

		profileID, err := getProfileID(m.profile)
		if err != nil {
			klog.Errorf("Failed to get profile ID: %v", err)
			continue
		}

		// 创建命令（使用profile ID和实际创建数量）
		profileArg := fmt.Sprintf("%d*%d", createCount, profileID)
		// 创建指定数量的MIG设备
		for i := 0; i < createCount; i++ {
			_, err := runNvidiaSmiCommand("mig", "-cgi", profileArg, "-C")
			if err != nil {
				klog.Errorf("Failed to create MIG device #%d on GPU %s: %v", i+1, index, err)
				break
			}
			klog.Infof("Successfully created MIG device #%d on GPU %s", i+1, index)
		}
	}

	return nil
}

func getProfileID(profileName string) (int, error) {
	out, err := runNvidiaSmiCommand("mig", "-lgip")
	if err != nil {
		return 0, err
	}

	// 正则表达式匹配profile行
	re := regexp.MustCompile(`\|\s+\d+\s+MIG\s+(\S+)\s+(\d+)`)

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		klog.Infof("Found line %s", line)
		// 跳过非profile行（表格线、标题等）
		if !strings.Contains(line, "MIG") || !strings.Contains(line, "|") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if len(matches) > 2 {
			name := matches[1]
			idStr := matches[2]

			if name == profileName {
				profileID, err := strconv.Atoi(idStr)
				if err != nil {
					klog.Warningf("Invalid profile ID format: %s", idStr)
					continue
				}
				klog.Infof("Found profile %s with ID %d", profileName, profileID)
				return profileID, nil
			}
		}
	}
	return 0, fmt.Errorf("profile not found: %s", profileName)
}

// 获取当前MIG设备数量
func (m *MIGManager) getMIGDeviceCount(gpuIndex string) (int, error) {
	out, err := runNvidiaSmiCommand("mig", "-lgi", "-i", gpuIndex)
	output := string(out)

	// 处理无 MIG 设备的情况
	if strings.Contains(output, "No GPU instances found") ||
		strings.Contains(output, "Not Found") ||
		strings.Contains(output, "No devices were found") {
		klog.Infof("No MIG instances found on GPU %s", gpuIndex)
		return 0, nil
	}

	if err != nil {
		// 检查是否因为无设备而返回错误
		if strings.Contains(err.Error(), "exit status 255") &&
			(strings.Contains(output, "No GPU instances found") ||
				strings.Contains(output, "Not Found")) {
			klog.Infof("No MIG devices on GPU %s (ignoring error)", gpuIndex)
			return 0, nil
		}
		return 0, fmt.Errorf("nvidia-smi MIG query failed: %v, output: %s", err, output)
	}

	// 解析输出中的设备计数
	count := 0
	lines := strings.Split(output, "\n")

	// 检测表头行
	headerFound := false
	for _, line := range lines {
		if strings.Contains(line, "GPU Instance ID") {
			headerFound = true
			continue
		}

		// 统计数据行
		if headerFound && strings.TrimSpace(line) != "" {
			count++
		}
	}

	return count, nil
}
