package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/benyuereal/micro-device-plugin/pkg/device"
	"github.com/benyuereal/micro-device-plugin/pkg/deviceplugin"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	// 初始化设备管理器
	managers := []struct {
		vendor  string
		manager device.DeviceManager
	}{
		{"nvidia", &device.NVIDIAManager{}},
		{"huawei", &device.HuaweiManager{}},
	}

	var servers []*deviceplugin.DevicePluginServer
	var wg sync.WaitGroup
	var serverMutex sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())

	// 为每个供应商启动插件
	for _, m := range managers {
		wg.Add(1)
		go func(vendor string, manager device.DeviceManager) {
			defer wg.Done()

			srv := deviceplugin.New(vendor, manager)
			if err := srv.Start(); err != nil {
				klog.Errorf("Failed to start %s device plugin: %v", vendor, err)
				return
			}

			serverMutex.Lock()
			servers = append(servers, srv)
			serverMutex.Unlock()

			// 后台运行健康检查
			go srv.HealthCheck(ctx, 30*time.Second)
		}(m.vendor, m.manager)
	}

	// 健康检查路由
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			klog.Fatalf("Health check server failed: %v", err)
		}
	}()
	klog.Info("Health check server started on :8080")

	// 等待终止信号
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan
	klog.Info("Received termination signal, shutting down...")

	// 关闭所有插件
	cancel()
	for _, srv := range servers {
		srv.Stop()
	}

	klog.Info("All device plugins stopped. Exiting.")
}
