// Package app 负责提供 Go 本地程序诊断接口。
package app

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/config"
	"goodhr5/local-agent-go/internal/response"
)

// DiagnosticPort 表示本地端口诊断结果。
type DiagnosticPort struct {
	Port    int    `json:"port"`
	Current bool   `json:"current"`
	Free    bool   `json:"free"`
	Message string `json:"message"`
}

// DiagnosticDir 表示本地目录诊断结果。
type DiagnosticDir struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
	Message  string `json:"message"`
}

// DiagnosticLock 表示浏览器 Profile 锁文件。
type DiagnosticLock struct {
	Profile string `json:"profile"`
	Path    string `json:"path"`
	Name    string `json:"name"`
}

// handleDiagnostics 返回本地程序诊断信息。
// w 为响应对象，r 为请求对象。
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	result := map[string]any{
		"checked_at": time.Now().UTC().Format(time.RFC3339Nano),
		"os":         goruntime.GOOS,
		"arch":       goruntime.GOARCH,
		"host":       s.cfg.Host,
		"port":       s.cfg.Port,
		"paths": map[string]DiagnosticDir{
			"data":        diagnoseDir(s.cfg.DataDir),
			"runtime":     diagnoseDir(s.cfg.RuntimeDir),
			"ocr":         diagnoseDir(s.cfg.OCRDir),
			"frontend":    diagnoseDir(s.cfg.FrontendDir),
			"profiles":    diagnoseDir(s.cfg.ProfilesDir),
			"downloads":   diagnoseDir(s.cfg.DownloadsDir),
			"screenshots": diagnoseDir(s.cfg.ScreenshotsDir),
		},
		"ports":         diagnosePorts(s.cfg.Host, s.cfg.Port),
		"runtime":       s.runtime.Status(),
		"ocr":           s.ocr.Status(),
		"worker":        s.worker.Status(),
		"profile_locks": findProfileLocks(s.cfg.ProfilesDir),
	}
	result["recommendations"] = diagnosticRecommendations(result)
	response.Success(w, result)
}

// diagnosePorts 检查 9001-9009 端口状态。
// host 为监听地址，currentPort 为当前服务端口。
func diagnosePorts(host string, currentPort int) []DiagnosticPort {
	if host == "" {
		host = config.DefaultHost
	}
	result := []DiagnosticPort{}
	for port := config.DefaultPort; port <= config.MaxPort; port++ {
		item := DiagnosticPort{Port: port, Current: port == currentPort}
		if item.Current {
			item.Free = false
			item.Message = "当前本地程序正在使用"
			result = append(result, item)
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			_ = ln.Close()
			item.Free = true
			item.Message = "可用"
		} else {
			item.Free = false
			item.Message = "已被占用或暂不可监听：" + err.Error()
		}
		result = append(result, item)
	}
	return result
}

// diagnoseDir 检查目录是否存在并可写。
// dir 为目录路径。
func diagnoseDir(dir string) DiagnosticDir {
	item := DiagnosticDir{Path: dir}
	if strings.TrimSpace(dir) == "" {
		item.Message = "目录为空"
		return item
	}
	info, err := os.Stat(dir)
	if err != nil {
		item.Message = "目录不存在或不可访问：" + err.Error()
		return item
	}
	item.Exists = info.IsDir()
	if !item.Exists {
		item.Message = "路径不是目录"
		return item
	}
	testPath := filepath.Join(dir, ".goodhr-write-test")
	if err := os.WriteFile(testPath, []byte("ok"), 0o644); err != nil {
		item.Message = "目录不可写：" + err.Error()
		return item
	}
	_ = os.Remove(testPath)
	item.Writable = true
	item.Message = "正常"
	return item
}

// findProfileLocks 查找浏览器 Profile 残留锁文件。
// profilesDir 为 Profile 根目录。
func findProfileLocks(profilesDir string) []DiagnosticLock {
	locks := []DiagnosticLock{}
	if strings.TrimSpace(profilesDir) == "" {
		return locks
	}
	lockNames := map[string]struct{}{
		"SingletonLock":   {},
		"SingletonCookie": {},
		"SingletonSocket": {},
		"lockfile":        {},
	}
	_ = filepath.WalkDir(profilesDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if _, ok := lockNames[entry.Name()]; !ok {
			return nil
		}
		profile := filepath.Base(filepath.Dir(path))
		locks = append(locks, DiagnosticLock{Profile: profile, Path: path, Name: entry.Name()})
		if len(locks) >= 200 {
			return filepath.SkipAll
		}
		return nil
	})
	return locks
}

// diagnosticRecommendations 根据诊断结果生成中文建议。
// result 为诊断结果。
func diagnosticRecommendations(result map[string]any) []string {
	recommendations := []string{}
	if locks, ok := result["profile_locks"].([]DiagnosticLock); ok && len(locks) > 0 {
		recommendations = append(recommendations, fmt.Sprintf("发现 %d 个浏览器 Profile 锁文件，如确认浏览器已关闭，可先重启本地程序再试", len(locks)))
	}
	if runtimeStatus, ok := result["runtime"].(any); ok && runtimeStatus == nil {
		recommendations = append(recommendations, "运行组件状态为空，请检查 runtime 目录")
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "未发现明显异常")
	}
	return recommendations
}
