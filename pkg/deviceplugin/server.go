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
	vendor     string
	resource   string
	socket     string
	stop       chan struct{}
	healthChan chan string
	allocator  allocator.Allocator
	manager    device.DeviceManager
	server     *grpc.Server
}

func New(vendor string, manager device.DeviceManager) *DevicePluginServer {
	return &DevicePluginServer{
		vendor:     vendor,
		resource:   vendor + ".com/gpu",
		socket:     path.Join(pluginapi.DevicePluginPath, socketPrefix+"."+vendor),
		stop:       make(chan struct{}),
		healthChan: make(chan string, 1),
		manager:    manager,
		allocator:  allocator.NewSimpleAllocator(),
	}
}

// ListAndWatch 实现设备插件服务
func (s *DevicePluginServer) ListAndWatch(_ *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
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
			if err := s.updateDeviceList(stream); err != nil {
				return err
			}
		case id := <-s.healthChan:
			if err := s.updateDeviceList(stream); err != nil {
				return err
			}
			klog.Warningf("Device %s health status changed", id)
		case <-s.stop:
			return nil
		}
	}
}

func (s *DevicePluginServer) updateDeviceList(stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	devices, err := s.manager.DiscoverGPUs()
	if err != nil {
		return fmt.Errorf("failed to discover devices: %v", err)
	}

	deviceList := make([]*pluginapi.Device, len(devices))
	for i, d := range devices {
		// 更新设备健康状态
		healthy := s.manager.CheckHealth(d.ID())
		state := pluginapi.Healthy
		if !healthy {
			state = pluginapi.Unhealthy
		}

		deviceList[i] = &pluginapi.Device{
			ID:     d.ID(),
			Health: state,
		}
	}

	klog.Infof("Updating device list for %s: %d devices", s.vendor, len(deviceList))
	return stream.Send(&pluginapi.ListAndWatchResponse{Devices: deviceList})
}

// Allocate 设备分配实现
func (s *DevicePluginServer) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	response := pluginapi.AllocateResponse{}

	for _, containerReq := range req.ContainerRequests {
		containerResp := new(pluginapi.ContainerAllocateResponse)

		// 收集所有请求的设备ID
		var deviceIDs []string
		for _, id := range containerReq.DevicesIDs {
			deviceIDs = append(deviceIDs, id)
		}

		// 尝试分配这些设备
		if err := s.allocator.Allocate(deviceIDs); err != nil {
			return nil, fmt.Errorf("allocation failed: %v", err)
		}

		// 设置环境变量
		containerResp.Envs = map[string]string{
			s.resource + "_DEVICE_IDS": strings.Join(deviceIDs, ","),
		}

		// 添加设备挂载
		for _, id := range deviceIDs {
			devices, _ := s.manager.DiscoverGPUs()
			var devicePath string
			for _, d := range devices {
				if d.ID() == id {
					devicePath = d.GetPath()
					break
				}
			}

			if devicePath != "" {
				containerResp.Devices = append(containerResp.Devices, &pluginapi.DeviceSpec{
					HostPath:      devicePath,
					ContainerPath: devicePath,
					Permissions:   "rw",
				})
			}
		}

		response.ContainerResponses = append(response.ContainerResponses, containerResp)
	}

	return &response, nil
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
	// 确保插件目录存在
	if err := os.MkdirAll(pluginapi.DevicePluginPath, 0755); err != nil {
		return fmt.Errorf("failed to create device plugin directory: %v", err)
	}

	// 清理现有的socket文件
	if err := syscall.Unlink(s.socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to unlink socket: %v", err)
	}

	// 创建监听
	lis, err := net.Listen("unix", s.socket)
	if err != nil {
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
		return fmt.Errorf("failed to start gRPC server: %v", err)
	}

	// 注册到kubelet
	if err := s.registerWithKubelet(); err != nil {
		return fmt.Errorf("failed to register with kubelet: %v", err)
	}

	klog.Infof("%s device plugin started and registered with resource name %s", s.vendor, s.resource)
	return nil
}

// Stop 停止设备插件
func (s *DevicePluginServer) Stop() {
	close(s.stop)
	if s.server != nil {
		s.server.Stop()
	}
}

// HealthCheck 后台健康检查
func (s *DevicePluginServer) HealthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			devices, _ := s.manager.DiscoverGPUs()
			for _, d := range devices {
				currentHealth := d.IsHealthy()
				actualHealth := s.manager.CheckHealth(d.ID())

				if currentHealth != actualHealth {
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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if conn, err := net.Dial("unix", socket); err == nil {
				conn.Close()
				return nil
			}
			time.Sleep(restartDelay)
		}
	}
}
