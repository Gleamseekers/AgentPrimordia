package a2a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// v3.5 开放协议服务器互操作端点

// OpenInteropServer 开放协议兼容服务器
type OpenInteropServer struct {
	card     OpenAgentCard
	cfg      InteropConfig
	executor TaskExecutor
	tasks    map[string]*OpenTask
	mu       sync.RWMutex
	mux      *http.ServeMux

	// SSE 订阅管理
	subscribers map[string][]chan []byte
	subMu       sync.RWMutex
}

// NewOpenInteropServer 创建开放协议服务器
func NewOpenInteropServer(card OpenAgentCard, cfg InteropConfig) *OpenInteropServer {
	s := &OpenInteropServer{
		card:        card,
		cfg:         cfg,
		tasks:       make(map[string]*OpenTask),
		mux:         http.NewServeMux(),
		subscribers: make(map[string][]chan []byte),
	}
	s.registerRoutes()
	return s
}

// WithExecutor 设置任务执行器。
func (s *OpenInteropServer) WithExecutor(executor TaskExecutor) *OpenInteropServer {
	s.executor = executor
	return s
}

// registerRoutes 注册开放协议端点
func (s *OpenInteropServer) registerRoutes() {
	if s.cfg.ExposeAgentCard {
		s.mux.HandleFunc(s.cfg.AgentCardPath, s.handleAgentCard)
	}
	s.mux.HandleFunc("/a2a/v1", s.handleJSONRPC)
	s.mux.HandleFunc("/a2a/v1/tasks/{id}/events", s.handleTaskEvents)
}

// Handler 返回 HTTP Handler
func (s *OpenInteropServer) Handler() http.Handler {
	return s.mux
}

// handleAgentCard 处理 Agent Card 请求
func (s *OpenInteropServer) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.card)
}

// handleJSONRPC 处理 JSON-RPC 请求
func (s *OpenInteropServer) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		ID      any             `json:"id"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInteropJSONRPCError(w, nil, OpenErrParseError, "Parse error")
		return
	}

	switch req.Method {
	case "tasks/send":
		s.handleTaskSend(w, req.ID, req.Params)
	case "tasks/get":
		s.handleTaskGet(w, req.ID, req.Params)
	case "tasks/cancel":
		s.handleTaskCancel(w, req.ID, req.Params)
	default:
		writeInteropJSONRPCError(w, req.ID, OpenErrMethodNotFound, "Method not found: "+req.Method)
	}
}

func (s *OpenInteropServer) handleTaskSend(w http.ResponseWriter, id any, params json.RawMessage) {
	var p struct {
		ID       string        `json:"id"`
		Message  OpenMessage   `json:"message"`
		Metadata map[string]any `json:"metadata"`
	}
	_ = json.Unmarshal(params, &p)

	taskID := p.ID
	if taskID == "" {
		taskID = "task-" + randomHex(8)
	}

	task := &OpenTask{
		ID:       taskID,
		Status:   OpenTaskStatus{State: OpenTaskWorking, Timestamp: time.Now()},
		Messages: []OpenMessage{p.Message},
		Metadata: p.Metadata,
	}

	s.mu.Lock()
	s.tasks[task.ID] = task
	snapshot := cloneOpenTask(task) // 锁内快照，锁外序列化（防 executeTask 并发改写）
	s.mu.Unlock()

	// 异步执行任务
	if s.executor != nil {
		go s.executeTask(task)
	}

	writeInteropJSONRPCResult(w, id, snapshot)
}

func (s *OpenInteropServer) executeTask(task *OpenTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := s.executor.Execute(ctx, task)

	s.mu.Lock()
	if err != nil {
		task.Status = OpenTaskStatus{State: OpenTaskFailed, Timestamp: time.Now()}
	} else if result != nil {
		*task = *result
	}
	s.mu.Unlock()

	// 广播 SSE 事件（传快照，防与后续读写竞争）
	s.broadcastTaskEvent(cloneOpenTask(task))
}

func (s *OpenInteropServer) handleTaskGet(w http.ResponseWriter, id any, params json.RawMessage) {
	var p struct {
		TaskID string `json:"taskId"`
	}
	_ = json.Unmarshal(params, &p)

	s.mu.RLock()
	task, ok := s.tasks[p.TaskID]
	var snapshot *OpenTask
	if ok {
		snapshot = cloneOpenTask(task) // RLock 内快照，锁外序列化
	}
	s.mu.RUnlock()

	if !ok {
		writeInteropJSONRPCError(w, id, OpenErrTaskNotFound, "Task not found")
		return
	}
	writeInteropJSONRPCResult(w, id, snapshot)
}

func (s *OpenInteropServer) handleTaskCancel(w http.ResponseWriter, id any, params json.RawMessage) {
	var p struct {
		TaskID string `json:"taskId"`
	}
	_ = json.Unmarshal(params, &p)

	s.mu.Lock()
	task, ok := s.tasks[p.TaskID]
	if !ok {
		s.mu.Unlock()
		writeInteropJSONRPCError(w, id, OpenErrTaskNotFound, "Task not found")
		return
	}
	task.Status = OpenTaskStatus{State: OpenTaskCanceled, Timestamp: time.Now()}
	snapshot := cloneOpenTask(task) // 锁内快照
	s.mu.Unlock()

	if s.executor != nil {
		_ = s.executor.Cancel(context.Background(), p.TaskID)
	}

	s.broadcastTaskEvent(snapshot)
	writeInteropJSONRPCResult(w, id, snapshot)
}

// handleTaskEvents SSE 流端点
func (s *OpenInteropServer) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if taskID == "" {
		http.Error(w, "task ID required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 创建订阅 channel
	ch := make(chan []byte, 16)
	s.subMu.Lock()
	s.subscribers[taskID] = append(s.subscribers[taskID], ch)
	s.subMu.Unlock()

	defer func() {
		s.subMu.Lock()
		subs := s.subscribers[taskID]
		for i, sub := range subs {
			if sub == ch {
				s.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		s.subMu.Unlock()
		close(ch)
	}()

	// 发送当前任务状态（RLock 内快照，锁外序列化/判断）
	s.mu.RLock()
	task, exists := s.tasks[taskID]
	var snapshot *OpenTask
	if exists {
		snapshot = cloneOpenTask(task)
	}
	s.mu.RUnlock()

	if exists {
		data, _ := json.Marshal(snapshot)
		fmt.Fprintf(w, "event: task\ndata: %s\n\n", data)
		flusher.Flush()

		if st := snapshot.Status.State; st == OpenTaskCompleted || st == OpenTaskFailed || st == OpenTaskCanceled {
			return
		}
	}

	// 等待事件
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: task\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// broadcastTaskEvent 向所有订阅者广播任务事件。
// 调用方必须传入任务快照（cloneOpenTask）——本函数在 subMu 之外
// Marshal，直接传共享 task 会与 executeTask 的锁内写竞争。
func (s *OpenInteropServer) broadcastTaskEvent(task *OpenTask) {
	data, err := json.Marshal(task)
	if err != nil {
		return
	}

	s.subMu.RLock()
	defer s.subMu.RUnlock()

	for _, ch := range s.subscribers[task.ID] {
		select {
		case ch <- data:
		default:
			// 慢消费者，丢弃事件
		}
	}
}

// cloneOpenTask 返回 OpenTask 的深拷只剩（含切片/映射）。
// 共享 task 的并发读写（executeTask 锁内写 vs 响应/SSE 锁外读）
// 曾造成数据竞争（-race 实测）；统一"锁内快照、锁外使用"后消除。
func cloneOpenTask(t *OpenTask) *OpenTask {
	if t == nil {
		return nil
	}
	cp := *t
	if t.Messages != nil {
		cp.Messages = make([]OpenMessage, len(t.Messages))
		copy(cp.Messages, t.Messages)
	}
	if t.Artifacts != nil {
		cp.Artifacts = make([]OpenArtifact, len(t.Artifacts))
		copy(cp.Artifacts, t.Artifacts)
	}
	if t.Metadata != nil {
		cp.Metadata = make(map[string]any, len(t.Metadata))
		for k, v := range t.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

// --- JSON-RPC 响应辅助 ---

func writeInteropJSONRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func writeInteropJSONRPCError(w http.ResponseWriter, id any, code OpenErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}

// randomHex 生成 n 字节随机数的十六进制字符串
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString(make([]byte, n))
	}
	return hex.EncodeToString(b)
}
