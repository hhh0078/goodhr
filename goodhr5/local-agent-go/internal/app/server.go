// Package app 负责注册 Go 版本本地程序 HTTP 服务和路由。
package app

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/process"
	"goodhr5/local-agent-go/internal/response"
	"goodhr5/local-agent-go/internal/runtime"
)

// Server 是 Go 版本 Local Agent HTTP 服务。
type Server struct {
	cfg     *config.Config
	runtime *runtime.Manager
	worker  *browser.WorkerManager
}

// NewServer 创建本地 HTTP 服务。
// cfg 为本地程序配置。
func NewServer(cfg *config.Config) *Server {
	runtimeManager := runtime.NewManager(cfg)
	return &Server{
		cfg:     cfg,
		runtime: runtimeManager,
		worker:  browser.NewWorkerManager(runtimeManager),
	}
}

// Run 启动本地 HTTP 服务。
// 会优先监听配置端口，失败时尝试到 9009。
func (s *Server) Run() error {
	ln, port, err := process.ListenFirstAvailable(s.cfg.Host, s.cfg.Port, config.MaxPort)
	if err != nil {
		return err
	}
	s.cfg.Port = port
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	server := &http.Server{
		Handler:           s.withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("GoodHR 5 Go Local Agent started on http://%s", net.JoinHostPort(s.cfg.Host, strconv.Itoa(port)))
	return server.Serve(ln)
}

// registerRoutes 注册本地程序路由。
// mux 为 HTTP 路由器。
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/runtime/status", s.handleRuntimeStatus)
	mux.HandleFunc("/api/v1/runtime/ensure", s.handleRuntimeEnsure)
	mux.HandleFunc("/api/v1/runtime/install", s.handleRuntimeInstall)
	mux.HandleFunc("/api/v1/worker/start", s.handleWorkerStart)
	mux.HandleFunc("/api/v1/worker/stop", s.handleWorkerStop)
	mux.HandleFunc("/api/v1/worker/status", s.handleWorkerStatus)
	mux.HandleFunc("/api/v1/browser/start", s.handleBrowserStart)
	mux.HandleFunc("/api/v1/browser/stop", s.handleBrowserStop)
	mux.HandleFunc("/api/v1/page/open", s.handlePageOpen)
	mux.HandleFunc("/api/v1/page/click", s.handlePageClick)
	mux.HandleFunc("/api/v1/page/type", s.handlePageType)
	mux.HandleFunc("/", s.handleConsole)
}

// handleHealth 返回本地程序健康状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, map[string]any{
		"status":  "ok",
		"version": "go-v2-dev",
		"port":    s.cfg.Port,
		"dataDir": s.cfg.DataDir,
	})
}

// handleRuntimeStatus 返回运行组件状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, s.runtime.Status())
}

// handleRuntimeEnsure 检查运行组件是否可用。
// w 为响应对象，r 为请求对象。
func (s *Server) handleRuntimeEnsure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	status, err := s.runtime.Ensure()
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, status)
}

// handleRuntimeInstall 根据 manifest 下载并安装运行组件。
// w 为响应对象，r 为请求对象。
func (s *Server) handleRuntimeInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	manifestURL := ""
	if value, ok := payload["manifest_url"].(string); ok {
		manifestURL = value
	}
	result, err := s.runtime.InstallFromManifest(r.Context(), manifestURL)
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, result)
}

// handleWorkerStart 启动 Node Browser Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleWorkerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	status, err := s.worker.Start(r.Context())
	if err != nil {
		response.Error(w, http.StatusConflict, err.Error())
		return
	}
	response.Success(w, status)
}

// handleWorkerStop 停止 Node Browser Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleWorkerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, s.worker.Stop())
}

// handleWorkerStatus 返回 Node Browser Worker 状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleWorkerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, s.worker.Status())
}

// handleBrowserStart 转发浏览器启动请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleBrowserStart(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/browser/start")
}

// handleBrowserStop 转发浏览器停止请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handleBrowserStop(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/browser/stop")
}

// handlePageOpen 转发页面打开请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageOpen(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/open")
}

// handlePageClick 转发页面点击请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageClick(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/click")
}

// handlePageType 转发页面输入请求给 Node Worker。
// w 为响应对象，r 为请求对象。
func (s *Server) handlePageType(w http.ResponseWriter, r *http.Request) {
	s.proxyWorkerPost(w, r, "/api/v1/page/type")
}

// handleConsole 返回本地控制台占位页面。
// w 为响应对象，r 为请求对象。
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>GoodHR Local Agent Go</title></head><body><h1>GoodHR Local Agent Go</h1><p>Go 版本本地程序已启动。</p></body></html>"))
}

// proxyWorkerPost 读取请求体并转发给 Node Worker。
// w 为响应对象，r 为请求对象，path 为 Worker API 路径。
func (s *Server) proxyWorkerPost(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	var payload map[string]any
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.worker.Call(r.Context(), path, payload)
	if err != nil {
		msg := "浏览器 Worker 调用失败"
		if err.Error() != "" {
			msg = err.Error()
		}
		response.Error(w, http.StatusBadGateway, msg)
		return
	}
	response.Success(w, result)
}

// readPayload 读取请求 JSON 参数。
// r 为 HTTP 请求对象，空 body 返回空 map。
func readPayload(r *http.Request) (map[string]any, error) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("请求参数不是有效 JSON")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

// withCORS 为本地 API 增加跨域响应头。
// next 为下一个 HTTP 处理器。
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-GoodHR-Agent-BaseURL")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == http.MethodOptions {
			response.Success(w, map[string]any{"ok": true})
			return
		}
		next.ServeHTTP(w, r)
	})
}
