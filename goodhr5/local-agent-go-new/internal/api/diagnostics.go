// Package api 文件作用：提供目录、端口、Profile 锁和运行组件的本地诊断结果。
package api

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

// diagnosticResult 表示控制台可直接展示的完整诊断结果。
type diagnosticResult struct {
	CheckedAt       string                    `json:"checked_at"`
	OS              string                    `json:"os"`
	Arch            string                    `json:"arch"`
	Host            string                    `json:"host"`
	Port            int                       `json:"port"`
	Paths           map[string]diagnosticPath `json:"paths"`
	Ports           []diagnosticPort          `json:"ports"`
	Runtime         diagnosticRuntime         `json:"runtime"`
	ProfileLocks    []diagnosticLock          `json:"profile_locks"`
	Recommendations []string                  `json:"recommendations"`
}

// diagnosticPath 表示一个本地目录的存在和可写状态。
type diagnosticPath struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
	Message  string `json:"message"`
}

// diagnosticPort 表示本地程序端口是否为当前端口或仍可监听。
type diagnosticPort struct {
	Port    int    `json:"port"`
	Current bool   `json:"current"`
	Free    bool   `json:"free"`
	Message string `json:"message"`
}

// diagnosticRuntime 表示本地运行组件的简短诊断状态。
type diagnosticRuntime struct {
	NodeReady   bool `json:"node_ready"`
	WorkerBuilt bool `json:"worker_built"`
	WorkerReady bool `json:"worker_ready"`
	OCRReady    bool `json:"ocr_ready"`
}

// diagnosticLock 表示 Chromium Profile 遗留锁文件。
type diagnosticLock struct {
	Profile string `json:"profile"`
	Path    string `json:"path"`
	Name    string `json:"name"`
}

// handleDiagnostics 返回目录、端口、Profile 锁和运行组件诊断。
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	runtimeStatus := diagnosticRuntime{
		NodeReady: s.runtime.CheckNode() == nil, WorkerBuilt: s.runtime.CheckWorkerBuild() == nil,
		WorkerReady: s.browser.Health(r.Context()) == nil, OCRReady: s.ocr.Ready() == nil,
	}
	result := diagnosticResult{
		CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
		OS:        goruntime.GOOS, Arch: goruntime.GOARCH, Host: s.cfg.Host, Port: s.cfg.Port,
		Paths: map[string]diagnosticPath{
			"data": diagnosePath(s.cfg.DataDir), "runtime": diagnosePath(s.cfg.RuntimeDir),
			"logs": diagnosePath(s.cfg.LogsDir), "profiles": diagnosePath(s.cfg.ProfilesDir),
			"extensions": diagnosePath(s.cfg.ExtensionsDir),
			"downloads":  diagnosePath(s.cfg.DownloadsDir), "screenshots": diagnosePath(s.cfg.ScreenshotsDir),
		},
		Ports:   diagnosePorts(s.cfg.Host, s.cfg.Port, s.cfg.Port+8),
		Runtime: runtimeStatus, ProfileLocks: findProfileLocks(s.cfg.ProfilesDir),
	}
	result.Recommendations = diagnosticRecommendations(result)
	writeSuccess(w, http.StatusOK, result)
}

// diagnosePath 检查目录是否存在且可以创建临时文件。
func diagnosePath(directory string) diagnosticPath {
	result := diagnosticPath{Path: directory}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		result.Message = "目录不存在或不可访问"
		return result
	}
	result.Exists = true
	file, err := os.CreateTemp(directory, ".goodhr-write-*")
	if err != nil {
		result.Message = "目录暂时不可写"
		return result
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	result.Writable = true
	result.Message = "正常"
	return result
}

// diagnosePorts 检查本地程序候选端口是否可监听。
func diagnosePorts(host string, currentPort int, maximumPort int) []diagnosticPort {
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	if maximumPort < currentPort {
		maximumPort = currentPort
	}
	result := make([]diagnosticPort, 0, maximumPort-currentPort+1)
	for port := currentPort; port <= maximumPort; port++ {
		item := diagnosticPort{Port: port, Current: port == currentPort}
		if item.Current {
			item.Message = "当前本地程序正在使用"
			result = append(result, item)
			continue
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			item.Message = "已被占用或暂不可监听"
		} else {
			item.Free = true
			item.Message = "可用"
			_ = listener.Close()
		}
		result = append(result, item)
	}
	return result
}

// findProfileLocks 查找浏览器 Profile 中常见的残留锁文件。
func findProfileLocks(profilesDir string) []diagnosticLock {
	lockNames := map[string]bool{
		"SingletonLock": true, "SingletonCookie": true, "SingletonSocket": true, "lockfile": true,
	}
	result := make([]diagnosticLock, 0)
	_ = filepath.WalkDir(profilesDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !lockNames[entry.Name()] {
			return nil
		}
		result = append(result, diagnosticLock{
			Profile: filepath.Base(filepath.Dir(path)), Path: path, Name: entry.Name(),
		})
		if len(result) >= 200 {
			return filepath.SkipAll
		}
		return nil
	})
	return result
}

// diagnosticRecommendations 根据强类型诊断结果生成简短处理建议。
func diagnosticRecommendations(result diagnosticResult) []string {
	recommendations := make([]string, 0)
	if len(result.ProfileLocks) > 0 {
		recommendations = append(recommendations, "发现浏览器 Profile 锁文件；确认浏览器已关闭后，可以重启本地程序再试")
	}
	if !result.Runtime.NodeReady || !result.Runtime.WorkerBuilt {
		recommendations = append(recommendations, "运行组件还没准备完整，可以在组件信息页继续安装")
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "暂时没发现明显问题，我先安静待命")
	}
	return recommendations
}
