// Command studio 启动 AgentPrimordia Studio 后端 HTTP 服务。
//
// 提供 studio/web 四面板（Chaos Lab / Cluster / Learning / Marketplace）
// 依赖的 /api/v1/* 端点。
//
// v7.4 起**默认接入真实引擎**：chaos(ChaosEngine) / cluster(ClusterManager) /
// learning(SelfModel) / marketplace(持久化 TemplateRegistry) / autonomy(AutonomyRuntime) /
// skills(Store) / realtime(Hub+EventBus)。传入 -demo 可回退到内置 demo 数据。
//
// 用法：
//
//	go run ./cmd/studio -addr :8090
//	go run ./cmd/studio -addr :8090 -token <bearer>   # 启用鉴权
//	go run ./cmd/studio -demo                          # 回退 demo 数据
//
// Studio Web 开发模式：npm run dev（vite.config.ts 已将 /api 代理到 :8090）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"agentprimordia/internal/agent/autonomy"
	"agentprimordia/internal/agent/cluster"
	agentmarket "agentprimordia/internal/agent/marketplace"
	"agentprimordia/internal/agent/realtime"
	"agentprimordia/internal/agent/skills"
	"agentprimordia/internal/chaos"
	"agentprimordia/internal/memory"
	"agentprimordia/internal/studio"
)

// studioDataDir Studio 本地持久化目录（var 便于测试注入临时目录）。
var studioDataDir = ".ap-studio"

func main() {
	addr := flag.String("addr", ":8090", "Studio 后端监听地址")
	token := flag.String("token", "", "可选：Bearer 访问令牌，设置后保护所有 /api/v1/* 端点")
	demo := flag.Bool("demo", false, "回退使用内置 demo 数据（默认接入真实引擎）")
	flag.Parse()

	handler := buildStudioHandler(*demo)

	server := &http.Server{
		Addr:    *addr,
		Handler: withOptionalAuth(handler, *token),
	}

	go func() {
		fmt.Printf("Studio 后端监听于 %s\n", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("\n正在关闭服务器...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("服务器关闭出错: %v", err)
	}
	fmt.Println("服务器已优雅退出")
}

// studioStepExecutor Studio 自治面板的最小步骤执行器（确定性、无副作用）。
type studioStepExecutor struct{}

func (studioStepExecutor) ExecuteStep(_ context.Context, step autonomy.PlanStep) (string, error) {
	return "ok: " + step.ID, nil
}

// buildStudioHandler 装配 Studio。demo=true 时使用内置 demo 数据；
// 否则注入真实引擎适配器（v7.4 接线）。
func buildStudioHandler(demo bool) *studio.StudioHandler {
	if demo {
		fmt.Println("Studio 运行于 demo 模式（-demo）：面板数据为内置模拟数据")
		return studio.NewStudioHandler()
	}

	regPath := filepath.Join(studioDataDir, "templates.json")
	templateRegistry := agentmarket.NewTemplateRegistry(
		agentmarket.WithStore(agentmarket.NewJSONFileStore(regPath)),
	)
	autonomyRT := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{
		StepExecutor: studioStepExecutor{},
	})

	fmt.Printf("Studio 已接入真实引擎（模板注册表: %s）\n", regPath)
	return studio.NewStudioHandler(
		studio.WithChaos(studio.NewChaosServiceAdapter(chaos.NewEngine())),
		studio.WithCluster(studio.NewClusterServiceAdapter(cluster.NewClusterManager(cluster.ClusterConfig{
			NodeID:     "studio-local",
			ListenAddr: "127.0.0.1:0",
		}))),
		studio.WithLearning(studio.NewRealLearningService(memory.NewSelfModel())),
		studio.WithMarketplace(studio.NewMarketplaceServiceAdapter(templateRegistry)),
		studio.WithAutonomy(studio.NewAutonomyServiceAdapter(autonomyRT)),
		studio.WithSkills(studio.NewSkillServiceAdapter(skills.NewStore())),
		studio.WithRealtime(studio.NewRealtimeServiceAdapter(realtime.NewRealtimeHub(realtime.HubConfig{}), realtime.NewEventBus())),
	)
}

// withOptionalAuth 在设置了 -token 时对 /api/v1/* 端点做 Bearer 鉴权。
func withOptionalAuth(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
