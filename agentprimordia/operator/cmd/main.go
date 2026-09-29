package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
	"github.com/Gleamseekers/AgentPrimordia/operator/controller"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(agentv1.AddToScheme(scheme))
}

func main() {
	metricsAddr := flag.String("metrics-addr", ":8080", "Metrics 服务器监听地址")
	healthAddr := flag.String("health-addr", ":8081", "健康检查服务器监听地址（healthz/readyz）")
	enableLeaderElection := flag.Bool("leader-elect", false, "启用 Leader 选举")
	flag.Parse()

	cfg, err := config.GetConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 K8s 配置失败: %v\n", err)
		fmt.Fprintln(os.Stderr, "请确保 kubeconfig 已配置或运行在集群内")
		os.Exit(1)
	}

	mgr, err := manager.New(cfg, manager.Options{
		Scheme:                 scheme,
		Metrics:                server.Options{BindAddress: *metricsAddr},
		HealthProbeBindAddress: *healthAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "agentprimordia-operator",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建 Manager 失败: %v\n", err)
		os.Exit(1)
	}

	// 注册健康检查端点
	if err := mgr.AddHealthzCheck("healthz", func(req *http.Request) error {
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "注册 healthz 检查失败: %v\n", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "注册 readyz 检查失败: %v\n", err)
		os.Exit(1)
	}

	// 注册 Controller
	if err := (&controller.AgentDeploymentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "注册 Controller 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("AgentPrimordia Operator 启动...")
	fmt.Printf("Metrics: %s\n", *metricsAddr)
	fmt.Printf("Health:  %s\n", *healthAddr)

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		fmt.Fprintf(os.Stderr, "Operator 运行失败: %v\n", err)
		os.Exit(1)
	}
}
