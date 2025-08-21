package deviceplugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/benyuereal/micro-device-plugin/pkg/allocator"
	"github.com/benyuereal/micro-device-plugin/pkg/device"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	socketPrefix  = "microui.sock"
	kubeletSocket = pluginapi.KubeletSocket
	restartDelay  = 5 * time.Second
)

type DevicePluginServer struct {
	vendor          string
	resource        string
	socket          string
	stop            chan struct{}
	healthChan      chan string
	allocator       allocator.Allocator
	manager         device.DeviceManager
	server          *grpc.Server
	lastDeviceState map[string]string // 使用字符串记录健康状态
}

func New(vendor string, manager device.DeviceManager) *DevicePluginServer {
	return &DevicePluginServer{
		vendor:          vendor,
		resource:        vendor + ".com/microgpu",
		socket:          path.Join(pluginapi.DevicePluginPath, socketPrefix+"."+vendor),
		stop:            make(chan struct{}),
		healthChan:      make(chan string, 1),
		manager:         manager,
		allocator:       allocator.NewSimpleAllocator(),
		lastDeviceState: make(map[string]string),
	}
}

// ListAndWatch 实现设备插件服务
func (s *DevicePluginServer) ListAndWatch(_ *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	klog.Infof("Starting ListAndWatch for %s device plugin", s.vendor)

	// 初始设备列表
	if err := s.updateDeviceList(stream); err != nil {
		return err
	}

	// 定时更新和健康检查
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			klog.V(5).Infof("Periodic device list update for %s", s.vendor)
			if err := s.updateDeviceList(stream); err != nil {
				return err
			}
		case id := <-s.healthChan:
			klog.Warningf("Device %s health status changed, updating device list", id)
			if err := s.updateDeviceList(stream); err != nil {
				return err
			}
		case <-s.stop:
			klog.Infof("Stopping ListAndWatch for %s device plugin", s.vendor)
			return nil
		}
	}
}

func (s *DevicePluginServer) updateDeviceList(stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	devices, err := s.manager.DiscoverGPUs()
	if err != nil {
		klog.Errorf("Failed to discover devices: %v", err)
		return fmt.Errorf("failed to discover devices: %v", err)
	}
	// 新增：清理已消失设备的分配状态
	discoveredIDs := make(map[string]bool)
	for _, d := range devices {
		discoveredIDs[d.ID()] = true
	}
	s.allocator.CleanupOrphanedDevices(discoveredIDs)

	deviceList := make([]*pluginapi.Device, len(devices))
	healthStatusCount := map[string]int{
		pluginapi.Healthy:   0,
		pluginapi.Unhealthy: 0}

	for i, d := range devices {
		// 更新设备健康状态
		healthy := s.manager.CheckHealth(d.ID())
		state := pluginapi.Healthy
		if !healthy {
			state = pluginapi.Unhealthy
		}
		healthStatusCount[state]++

		// 记录状态变化
		if prevState, exists := s.lastDeviceState[d.ID()]; exists && prevState != state {
			klog.Infof("Device %s health changed from %s to %s", d.ID(), prevState, state)
		}
		s.lastDeviceState[d.ID()] = state

		deviceList[i] = &pluginapi.Device{
			ID:     d.ID(),
			Health: state,
		}
	}

	klog.Infof("Updating device list for %s: %d devices (%d healthy, %d unhealthy)",
		s.vendor, len(deviceList), healthStatusCount[pluginapi.Healthy], healthStatusCount[pluginapi.Unhealthy])

	return stream.Send(&pluginapi.ListAndWatchResponse{Devices: deviceList})
}

// Allocate 设备分配实现
func (s *DevicePluginServer) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	klog.Infof("Received Allocate request for %s: %v", s.resource, req.ContainerRequests)

	response := pluginapi.AllocateResponse{}

	for _, containerReq := range req.ContainerRequests {
		containerResp := new(pluginapi.ContainerAllocateResponse)
		hasMIGDevice := false // 标记是否包含MIG设备
		// 收集所有请求的设备ID
		var deviceIDs []string
		for _, id := range containerReq.DevicesIDs {
			// 挂载物理GPU设备（如/dev/nvidia0）
			// 检测是否为MIG设备
			if s.isMIGDevice(id) {
				hasMIGDevice = true
			}
			deviceIDs = append(deviceIDs, id)
		}
		// 为MIG设备添加专用挂载
		if hasMIGDevice {
			containerResp.Devices = append(containerResp.Devices, &pluginapi.DeviceSpec{
				HostPath:      "/dev/nvidia-caps",
				ContainerPath: "/dev/nvidia-caps",
				Permissions:   "rw",
			})
		}

		klog.Infof("Allocating devices for container: %v", deviceIDs)

		// 尝试分配这些设备
		if err := s.allocator.Allocate(deviceIDs); err != nil {
			klog.Errorf("Allocation failed for devices %v: %v", deviceIDs, err)
			return nil, fmt.Errorf("allocation failed: %v", err)
		}

		// 添加 CUDA 库路径 (需在 DaemonSet 中定义)
		cudaLibPath := "/host-lib" // 宿主机 CUDA 库路径

		// 添加 CUDA 环境变量
		containerResp.Envs = map[string]string{
			"CUDA_VISIBLE_DEVICES": strings.Join(deviceIDs, ","), // 标准环境变量
			"LD_LIBRARY_PATH":      "/usr/local/cuda/lib64:" + cudaLibPath + ":$LD_LIBRARY_PATH",
		}

		// 添加 CUDA 库挂载
		containerResp.Mounts = append(containerResp.Mounts, &pluginapi.Mount{
			HostPath:      cudaLibPath,
			ContainerPath: cudaLibPath,
			ReadOnly:      true,
		})
		klog.Infof("Set environment variable: %s_DEVICE_IDS=%s", s.resource, containerResp.Envs[s.resource+"_DEVICE_IDS"])

		// 添加设备挂载
		for _, id := range deviceIDs {
			devices, _ := s.manager.DiscoverGPUs()
			var devicePath string
			for _, d := range devices {
				if d.ID() == id {
					devicePath = d.GetPath()
					klog.Infof("Device %s is on GPU %s", d.ID(), devicePath)
					break
				}
			}

			if devicePath != "" {
				containerResp.Devices = append(containerResp.Devices, &pluginapi.DeviceSpec{
					HostPath:      devicePath,
					ContainerPath: devicePath,
					Permissions:   "rw",
				})
				klog.Infof("Adding device mount for %s: %s", id, devicePath)
			} else {
				klog.Warningf("Device path not found for device ID: %s", id)
			}
		}

		response.ContainerResponses = append(response.ContainerResponses, containerResp)
	}

	klog.Infof("Allocation successful for %s", s.resource)
	return &response, nil
}

func (s *DevicePluginServer) isMIGDevice(id string) bool {
	devices, _ := s.manager.DiscoverGPUs()
	for _, d := range devices {
		if d.ID() == id && d.IsMIG() {
			return true
		}
	}
	return false
}

// GetDevicePluginOptions 插件选项
func (s *DevicePluginServer) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired: false,
	}, nil
}

// PreStartContainer 容器启动前预处理（可选）
func (s *DevicePluginServer) PreStartContainer(ctx context.Context, req *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// GetPreferredAllocation 分配偏好（可选）
func (s *DevicePluginServer) GetPreferredAllocation(ctx context.Context, req *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

// *********** 服务管理方法 ***********

// Start 启动设备插件服务
func (s *DevicePluginServer) Start() error {
	klog.Infof("Starting %s device plugin", s.vendor)

	// 如果是NVIDIA设备，配置MIG
	if nvidiaManager, ok := s.manager.(*device.NVIDIAManager); ok {
		nvidiaManager.ConfigureMIG()
	}

	// 确保插件目录存在
	if err := os.MkdirAll(pluginapi.DevicePluginPath, 0755); err != nil {
		klog.Errorf("Failed to create device plugin directory: %v", err)
		return fmt.Errorf("failed to create device plugin directory: %v", err)
	}

	// 清理现有的socket文件
	if err := syscall.Unlink(s.socket); err != nil && !os.IsNotExist(err) {
		klog.Errorf("Failed to unlink socket: %v", err)
		return fmt.Errorf("failed to unlink socket: %v", err)
	}

	// 创建监听
	lis, err := net.Listen("unix", s.socket)
	if err != nil {
		klog.Errorf("Failed to listen on socket: %v", err)
		return fmt.Errorf("failed to listen on socket: %v", err)
	}

	// 创建gRPC服务
	s.server = grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(s.server, s)

	// 启动gRPC服务
	go func() {
		klog.Infof("Starting %s device plugin server at: %s", s.vendor, s.socket)
		if err := s.server.Serve(lis); err != nil {
			klog.Fatalf("Device plugin server failed: %v", err)
		}
	}()

	// 等待服务器启动
	connCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := waitForSocket(connCtx, s.socket); err != nil {
		klog.Errorf("Failed to start gRPC server: %v", err)
		return fmt.Errorf("failed to start gRPC server: %v", err)
	}

	// 注册到kubelet
	if err := s.registerWithKubelet(); err != nil {
		klog.Errorf("Failed to register with kubelet: %v", err)
		return fmt.Errorf("failed to register with kubelet: %v", err)
	}

	klog.Infof("%s device plugin started and registered with resource name %s", s.vendor, s.resource)
	s.allocator = allocator.NewSimpleAllocator() // 确保分配器初始化
	return nil
}

// Stop 停止设备插件
func (s *DevicePluginServer) Stop() {
	klog.Infof("Stopping %s device plugin", s.vendor)
	close(s.stop)
	if s.server != nil {
		s.server.Stop()
	}
}

// HealthCheck 后台健康检查
func (s *DevicePluginServer) HealthCheck(ctx context.Context, interval time.Duration) {
	klog.Infof("Starting health check for %s plugin with interval %v", s.vendor, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			devices, err := s.manager.DiscoverGPUs()
			if err != nil {
				klog.Errorf("Failed to discover devices during health check: %v", err)
				continue
			}

			for _, d := range devices {
				currentHealth := d.IsHealthy()
				actualHealth := s.manager.CheckHealth(d.ID())

				if currentHealth != actualHealth {
					klog.Warningf("Device %s health status changed from %v to %v", d.ID(), currentHealth, actualHealth)
					s.healthChan <- d.ID()
				}
			}
		case <-ctx.Done():
			klog.Infof("Stopping health check for %s plugin", s.vendor)
			return
		}
	}
}

// *********** 辅助方法 ***********

func (s *DevicePluginServer) registerWithKubelet() error {
	klog.Infof("Registering with kubelet at %s", kubeletSocket)

	conn, err := grpc.Dial(kubeletSocket, grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", addr)
		}),
	)

	if err != nil {
		return fmt.Errorf("failed to connect to kubelet: %v", err)
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	req := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(s.socket),
		ResourceName: s.resource,
	}

	_, err = client.Register(context.Background(), req)
	return err
}

func waitForSocket(ctx context.Context, socket string) error {
	klog.V(4).Infof("Waiting for socket %s to be ready", socket)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if conn, err := net.Dial("unix", socket); err == nil {
				conn.Close()
				klog.V(4).Infof("Socket %s is ready", socket)
				return nil
			}
			time.Sleep(restartDelay)
		}
	}
}
