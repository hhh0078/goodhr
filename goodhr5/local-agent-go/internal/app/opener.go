// Package app 提供本地控制台启动后的默认浏览器打开逻辑。
package app

import (
	"log"
	"net"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"time"
)

// openConsoleAfterStart 在服务启动后打开控制台。
// port 为实际监听端口。
func (s *Server) openConsoleAfterStart(port int) {
	if !s.cfg.AutoOpenConsole {
		return
	}
	url := s.consoleURL(port)
	go func() {
		time.Sleep(400 * time.Millisecond)
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

// openDefaultBrowser 用系统默认浏览器打开控制台。
// url 为控制台地址。
func openDefaultBrowser(url string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
