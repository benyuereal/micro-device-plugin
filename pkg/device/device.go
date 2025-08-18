package device

// GPUDevice 表示GPU设备的接口
type GPUDevice interface {
	ID() string
	IsHealthy() bool
	GetVendor() string
	GetPath() string
}

// DeviceManager 设备管理器接口
type DeviceManager interface {
	DiscoverGPUs() ([]GPUDevice, error)
	CheckHealth(deviceID string) bool
}
