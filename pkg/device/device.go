package device

// GPUDevice 表示GPU设备的接口
type GPUDevice interface {
	ID() string
	IsHealthy() bool
	GetVendor() string
	GetPath() string
	IsMIG() bool        // 新增：是否为MIG设备
	PhysicalID() string // 新增：物理GPU ID
}

// DeviceManager 设备管理器接口
type DeviceManager interface {
	DiscoverGPUs() ([]GPUDevice, error)
	CheckHealth(deviceID string) bool
}

type SimulatorDevice struct {
	id      string
	healthy bool
}

func (d *SimulatorDevice) IsMIG() bool {
	return false
}

func (d *SimulatorDevice) PhysicalID() string {
	return d.id
}

func (d *SimulatorDevice) ID() string        { return d.id }
func (d *SimulatorDevice) IsHealthy() bool   { return d.healthy }
func (d *SimulatorDevice) GetVendor() string { return "simulator" }
func (d *SimulatorDevice) GetPath() string   { return "/dev/sim_gpu" + d.id }
