// Package startup 文件作用：在数据迁移前提供本机启动进度页面，独立于数据库和招聘浏览器。
package startup

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/system/console"
)

//go:embed page.html
var page []byte

// State 保存页面可读的启动状态，不含业务数据。
type State struct {
	config.DataProgress
	Error  string `json:"error,omitempty"`
	Target string `json:"target,omitempty"`
}

// View 管理仅监听回环地址、带随机路径的启动页面。
type View struct {
	mu        sync.RWMutex
	state     State
	server    *http.Server
	URL       string
	target    string
	Done      chan struct{}
	closeOnce sync.Once
}

// New 创建启动页面服务，不访问任何用户数据目录。
func New(consoleURL string, port int) (*View, error) {
	target, err := url.Parse(consoleURL)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("控制台地址不正确")
	}
	query := target.Query()
	query.Set("local_port", strconv.Itoa(port))
	target.RawQuery = query.Encode()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	prefix := "/" + hex.EncodeToString(nonce[:]) + "/"
	v := &View{Done: make(chan struct{}), URL: "http://" + listener.Addr().String() + prefix, target: target.String(), state: State{DataProgress: config.DataProgress{Stage: "checking", Message: "我先检查一下本地数据"}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+prefix, v.handle)
	mux.HandleFunc("POST "+prefix+"dismiss", v.dismiss)
	v.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = v.server.Serve(listener) }()
	return v, nil
}

// handle 返回启动页面和状态，禁用缓存并限制页面资源为本机内容。
func (v *View) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	base, _ := url.Parse(v.URL)
	switch r.URL.Path {
	case base.Path + "health":
		w.WriteHeader(http.StatusOK)
	case base.Path + "status":
		w.Header().Set("Content-Type", "application/json")
		v.mu.RLock()
		defer v.mu.RUnlock()
		_ = json.NewEncoder(w).Encode(v.state)
	case base.Path:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		_, _ = w.Write(page)
	default:
		http.NotFound(w, r)
	}
}

// Update 更新迁移阶段；页面自行轮询，不在复制线程中发送网络请求。
func (v *View) Update(p config.DataProgress) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state = State{DataProgress: p}
}

// Fail 保留错误页面，用户可查看具体原因并重新启动程序。
func (v *View) Fail(err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state.Stage = "failed"
	v.state.Message = "这次没准备好，原因在下面"
	v.state.Error = err.Error()
	v.state.Target = ""
}

// FollowReady 在业务健康接口确认可用后，让同一标签页进入原有控制台组件检查流程。
func (v *View) FollowReady(ctx context.Context, healthURL string, port int) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if console.ExistingAgent(ctx, healthURL, port) {
			v.mu.Lock()
			if v.state.Stage != "failed" {
				v.state = State{DataProgress: config.DataProgress{Stage: "ready", Message: "数据已准备好，接下来检查运行组件"}, Target: v.target}
			}
			v.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Close 关闭启动页监听，不影响业务服务或浏览器。
func (v *View) Close() { v.closeOnce.Do(func() { _ = v.server.Close(); close(v.Done) }) }

// dismiss 只允许当前本机页面关闭已失败的启动实例，不影响正常业务进程。
func (v *View) dismiss(w http.ResponseWriter, r *http.Request) {
	base, _ := url.Parse(v.URL)
	if r.Header.Get("Origin") != base.Scheme+"://"+base.Host {
		http.Error(w, "来源不匹配", http.StatusForbidden)
		return
	}
	v.mu.RLock()
	failed := v.state.Stage == "failed"
	v.mu.RUnlock()
	if !failed {
		http.Error(w, "程序仍在准备或运行", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	go v.Close()
}
