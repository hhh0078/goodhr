// Package app 提供本地控制台启动后的默认浏览器打开逻辑。
package app

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/cloudapi"
)

// openConsoleAfterStart 在本地服务可访问后打开控制台。
// port 为实际监听端口。
func (s *Server) openConsoleAfterStart(port int) {
	if !s.cfg.AutoOpenConsole {
		log.Printf("自动打开控制台已关闭：port=%d", port)
		return
	}
	url := s.consoleURL(port)
	healthURL := s.healthURL(port)
	log.Printf("准备自动打开控制台：fallback_url=%s health_url=%s", url, healthURL)
	go func() {
		if err := waitConsoleReady(healthURL, 6*time.Second); err != nil {
			log.Printf("本地服务还没准备好，暂不自动打开控制台，请手动访问 %s：%v", url, err)
			return
		}
		log.Printf("本地健康检查通过，准备解析控制台地址：fallback_url=%s", url)
		url = s.resolveConsoleURL(url)
		log.Printf("准备调用系统默认浏览器打开控制台：target_url=%s", url)
		if err := openDefaultBrowser(url); err != nil {
			log.Printf("打开控制台失败，请手动访问 %s：%v", url, err)
			return
		}
		log.Printf("已用默认浏览器打开控制台：%s", url)
	}()
}

// openExistingConsole 打开已运行本地程序对应的控制台。
// port 为旧实例监听端口，返回 true 表示已确认旧实例可用并尝试打开控制台。
func (s *Server) openExistingConsole(port int) bool {
	if !s.cfg.AutoOpenConsole {
		log.Printf("检测到旧实例但自动打开控制台已关闭：port=%d", port)
		return false
	}
	fallbackURL := s.consoleURL(port)
	if err := waitConsoleReady(s.healthURL(port), 800*time.Millisecond); err != nil {
		log.Printf("端口被占用但未确认旧实例可用：port=%d err=%v", port, err)
		return false
	}
	targetURL := s.resolveConsoleURL(fallbackURL)
	log.Printf("检测到旧实例可用，准备打开已有控制台：fallback_url=%s target_url=%s", fallbackURL, targetURL)
	if err := openDefaultBrowser(targetURL); err != nil {
		log.Printf("打开已运行控制台失败，请手动访问 %s：%v", targetURL, err)
		return true
	}
	log.Printf("已打开正在运行的控制台：%s", targetURL)
	return true
}

// consoleURL 返回本地控制台地址。
// port 为实际监听端口。
func (s *Server) consoleURL(port int) string {
	return "http://" + net.JoinHostPort(s.cfg.Host, strconv.Itoa(port)) + "/admin/"
}

// healthURL 返回本地服务健康检查地址。
// port 为实际监听端口。
func (s *Server) healthURL(port int) string {
	return "http://" + net.JoinHostPort(s.cfg.Host, strconv.Itoa(port)) + "/health"
}

// resolveConsoleURL 从云端公共配置读取控制台地址，失败时返回本地兜底地址。
// fallbackURL 为本地控制台地址。
func (s *Server) resolveConsoleURL(fallbackURL string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	log.Printf("开始读取云端控制台地址：cloud_api=%s fallback_url=%s", s.cfg.CloudAPIBase, fallbackURL)
	remoteURL, err := cloudapi.New(s.cfg.CloudAPIBase).FetchLocalAgentConsoleURL(ctx)
	if err != nil {
		log.Printf("读取云端控制台地址失败，使用本地控制台地址：%v", err)
		return fallbackURL
	}
	if !isConsoleURLAllowed(remoteURL) {
		if remoteURL != "" {
			log.Printf("云端控制台地址不合法，使用本地控制台地址：%s", remoteURL)
		}
		return fallbackURL
	}
	log.Printf("已读取云端控制台地址：%s", remoteURL)
	return remoteURL
}

// isConsoleURLAllowed 判断控制台地址是否可以交给系统浏览器打开。
// rawURL 为云端下发的原始地址。
func isConsoleURLAllowed(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// waitConsoleReady 等待本地服务可以响应健康检查。
// healthURL 为健康检查地址，timeout 为最长等待时间。
func waitConsoleReady(healthURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 800 * time.Millisecond}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("健康检查未返回成功状态")
}
