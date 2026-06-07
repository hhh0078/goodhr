// Package app 提供本地控制台启动后的桌面窗口和浏览器打开逻辑。
package app

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
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
		if err := openWailsConsole(url); err == nil {
			log.Printf("已打开 GoodHR 桌面控制台：%s", url)
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

// openWailsConsole 优先打开 Wails 桌面壳。
// url 为控制台地址。
func openWailsConsole(url string) error {
	candidates := wailsCommandCandidates()
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if err := startCommand(candidate, url); err == nil {
			return nil
		}
	}
	return fmt.Errorf("未找到可用的 Wails 控制台壳")
}

// wailsCommandCandidates 返回可能的 Wails 壳命令。
// 优先读取 GOODHR_WAILS_COMMAND，再查找同目录程序。
func wailsCommandCandidates() []string {
	result := []string{strings.TrimSpace(os.Getenv("GOODHR_WAILS_COMMAND"))}
	exePath, err := os.Executable()
	if err != nil {
		return result
	}
	dir := filepath.Dir(exePath)
	switch goruntime.GOOS {
	case "windows":
		result = append(result,
			filepath.Join(dir, "GoodHR Console.exe"),
			filepath.Join(dir, "goodhr-console.exe"),
		)
	case "darwin":
		result = append(result,
			filepath.Join(dir, "GoodHR Console.app"),
			filepath.Join(dir, "GoodHR Console"),
			filepath.Join(dir, "goodhr-console"),
		)
	default:
		result = append(result, filepath.Join(dir, "goodhr-console"))
	}
	return result
}

// startCommand 启动桌面壳命令。
// command 为命令或应用路径，url 为控制台地址。
func startCommand(command string, url string) error {
	if goruntime.GOOS == "darwin" && strings.HasSuffix(command, ".app") {
		if _, err := os.Stat(command); err != nil {
			return err
		}
		return exec.Command("open", command, "--args", url).Start()
	}
	if _, err := os.Stat(command); err != nil {
		if found, lookErr := exec.LookPath(command); lookErr == nil {
			command = found
		} else {
			return err
		}
	}
	return exec.Command(command, url).Start()
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
