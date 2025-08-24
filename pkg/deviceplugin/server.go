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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	lastDeviceState map[string]string           // 使用字符串记录健康状态
	deviceMap       map[string]device.GPUDevice // 设备ID到设备对象的映射
	cdiEnabled      bool
	cdiPrefix       string                // 添加CDI前缀配置
	kubeClient      *kubernetes.Clientset // 新增 Kubernetes 客户端
	nodeName        string                // 新增节点名称
}

func New(vendor string, manager device.DeviceManager, cdiEnabled bool, cdiPrefix string, nodeName string) *DevicePluginServer {
	// 创建 Kubernetes 客户端
	config, _ := rest.InClusterConfig()
	kubeClient, _ := kubernetes.NewForConfig(config)
	return &DevicePluginServer{
		vendor:          vendor,
		resource:        vendor + ".com/microgpu",
		socket:          path.Join(pluginapi.DevicePluginPath, socketPrefix+"."+vendor),
		stop:            make(chan struct{}),
		healthChan:      make(chan string, 1),
		manager:         manager,
		allocator:       allocator.NewSimpleAllocator(),
		lastDeviceState: make(map[string]string),
		deviceMap:       make(map[string]device.GPUDevice),
		cdiEnabled:      cdiEnabled,
		cdiPrefix:       cdiPrefix,
		kubeClient:      kubeClient,
		nodeName:        nodeName,
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

	// 修复：在更新设备列表时重建deviceMap
	newDeviceMap := make(map[string]device.GPUDevice)
	for _, d := range devices {
		newDeviceMap[d.ID()] = d
	}
	s.deviceMap = newDeviceMap
	klog.Infof("Discovered %d new devices, deviceMap %v", len(newDeviceMap), newDeviceMap)

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

// Allocate 设备分配实现 - 生产级MIG支持
func (s *DevicePluginServer) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	klog.Infof("Received Allocate request for %s: %v", s.resource, req.ContainerRequests)
	response := pluginapi.AllocateResponse{}

	// 修复：从请求的注解中获取 Pod UID（Kubernetes 标准方式）
	// 方法1: 尝试从环境变量获取 Pod 信息
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	podUID := ""
	if podName != "" && podNamespace != "" {
		pod, err := s.kubeClient.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			klog.Warningf("Failed to get pod %s/%s: %v", podNamespace, podName, err)
		} else {
			podUID = string(pod.UID)
			klog.Infof("Found pod UID via API: %s", podUID)
		}
	}

	for _, containerReq := range req.ContainerRequests {
		containerResp := new(pluginapi.ContainerAllocateResponse)

		// 获取 Pod UI
		// 尝试分配这些设备
		// 在分配设备前检查设备是否可用
		for _, devID := range containerReq.DevicesIDs {
			if !s.allocator.IsAvailable(devID) {
				// 如果设备已被分配但Pod不存在，清除错误状态
				if !s.isPodActive(s.allocator.GetPodUID(devID)) {
					s.allocator.Deallocate([]string{devID})
				} else {
					return nil, fmt.Errorf("device %s is already allocated", devID)
				}
			}
		}

		if err := s.allocator.Allocate(containerReq.DevicesIDs, podUID); err != nil {
			klog.Errorf("Allocation failed for devices %v: %v", containerReq.DevicesIDs, err)
			return nil, fmt.Errorf("allocation failed: %v", err)
		}

		// ================= 核心环境变量设置 =================
		envs := make(map[string]string)

		// 关键修改：使用物理索引而非设备ID
		envs["NVIDIA_VISIBLE_DEVICES"] = strings.Join(containerReq.DevicesIDs, ",")
		envs["NVIDIA_DRIVER_CAPABILITIES"] = "compute,utility,video,graphics"
		envs["NVIDIA_DISABLE_REQUIRE"] = "1"
		envs["NVIDIA_REQUIRE_MIG"] = "1"

		containerResp.Envs = envs

		// 打印环境变量用于调试
		for k, v := range containerResp.Envs {
			klog.Infof("Setting env: %s=%s", k, v)
		}

		response.ContainerResponses = append(response.ContainerResponses, containerResp)
	}

	klog.Infof("Allocation successful for %s, req :%v, resp: %v", s.resource, req.ContainerRequests,
		response.ContainerResponses)
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
func (s *DevicePluginServer) Start(ctx context.Context) error {
	klog.Infof("Starting %s device plugin", s.vendor)

	// 启动资源回收器（每 30 秒运行一次）
	go s.ResourceRecycler(ctx, 30*time.Second) // 共享主流程上下文
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

// 新增方法：资源回收器
func (s *DevicePluginServer) ResourceRecycler(ctx context.Context, interval time.Duration) {
	klog.Infof("Starting resource recycler for %s plugin", s.vendor)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:

			allocatedMap := s.allocator.GetAllocationMap() // 获取设备到 Pod 的映射
			if len(allocatedMap) == 0 {
				continue
			}

			// 检查已分配设备对应的 Pod
			var toRelease []string
			for deviceID, podUID := range allocatedMap {
				if podUID == "" {
					toRelease = append(toRelease, deviceID) // 无主设备直接释放
					continue
				}

				// 检查 Pod 状态：只有非活动状态（终止/完成）才释放
				if !s.isPodActive(podUID) {
					toRelease = append(toRelease, deviceID)
					klog.Infof("Marking device %s for release (pod %s is inactive)", deviceID, podUID)
				}
			}

			// 释放资源
			if len(toRelease) > 0 {
				s.allocator.Deallocate(toRelease)
				klog.Infof("Released %d orphaned devices, deivce %v", len(toRelease), toRelease)
			}

		case <-ctx.Done():
			klog.Infof("Stopping resource recycler for %s plugin", s.vendor)
			return
		}
	}
}

// isPodActive 检查 Pod 是否处于活动状态（非终止/完成）
func (s *DevicePluginServer) isPodActive(podUID string) bool {
	if podUID == "" {
		return false
	}
	pod, err := s.kubeClient.CoreV1().Pods("").Get(context.Background(), "", metav1.GetOptions{})
	if err != nil {
		klog.Warningf("Failed to get pod with UID %s: %v", podUID, err)
		return false // 默认按非活动处理
	}
	if pod.DeletionTimestamp != nil {
		return false // 正在终止，视为非活动
	}

	// 活动状态：Running 或 Pending
	if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
		return true
	}
	// 非活动状态：Succeeded（完成）、Failed（失败）或正在删除（DeletionTimestamp 非空）
	return false
}
