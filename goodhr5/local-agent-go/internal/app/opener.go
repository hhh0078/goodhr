// Package app 提供本地控制台启动后的默认浏览器打开逻辑。
package app

import (
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

// openConsoleAfterStart 在本地服务可访问后打开控制台。
// port 为实际监听端口。
func (s *Server) openConsoleAfterStart(port int) {
	if !s.cfg.AutoOpenConsole {
		return
	}
	url := s.consoleURL(port)
	healthURL := s.healthURL(port)
	go func() {
		if err := waitConsoleReady(healthURL, 6*time.Second); err != nil {
			log.Printf("本地服务还没准备好，暂不自动打开控制台，请手动访问 %s：%v", url, err)
			return
		}
		if err := openDefaultBrowser(url); err != nil {
			log.Printf("打开控制台失败，请手动访问 %s：%v", url, err)
			return
		}
		log.Printf("已用默认浏览器打开控制台：%s", url)
	}()
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
