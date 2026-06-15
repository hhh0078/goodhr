// Package ocr 负责调用本机 OCR 运行组件识别截图文字。
package ocr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go/internal/config"
)

// Engine 表示本地 OCR 引擎。
type Engine struct {
	cfg     *config.Config
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	logFile *os.File
	logPath string
	done    chan error
}

// Result 表示 OCR 识别结果。
type Result struct {
	Text string         `json:"text"`
	Raw  map[string]any `json:"raw,omitempty"`
}

// New 创建本地 OCR 引擎。
// cfg 为本地程序配置。
func New(cfg *config.Config) *Engine {
	return &Engine{cfg: cfg}
}

// Status 返回 OCR 组件状态。
// installed 表示是否找到 OCR 可执行文件。
func (e *Engine) Status() map[string]any {
	path := e.executablePath()
	return map[string]any{
		"installed": path != "",
		"path":      path,
		"dir":       e.cfg.OCRDir,
		"mode":      "rapidocr-json",
	}
}

// Recognize 识别图片文字。
// ctx 为请求上下文，imagePath 为图片绝对路径。
func (e *Engine) Recognize(ctx context.Context, imagePath string) (Result, error) {
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return Result{}, fmt.Errorf("图片路径不能为空")
	}
	if !filepath.IsAbs(imagePath) {
		return Result{}, fmt.Errorf("图片路径必须是绝对路径")
	}
	if _, err := os.Stat(imagePath); err != nil {
		return Result{}, fmt.Errorf("图片文件不存在：%s", imagePath)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureProcessLocked(); err != nil {
		return Result{}, err
	}
	payload := map[string]any{"image_path": imagePath}
	raw, _ := json.Marshal(payload)
	if _, err := e.stdin.Write(append(raw, '\n')); err != nil {
		e.stopLocked()
		return Result{}, fmt.Errorf("发送 OCR 请求失败：%w", err)
	}
	line, err := e.readJSONLineLocked(ctx)
	if err != nil {
		e.stopLocked()
		return Result{}, err
	}
	result := map[string]any{}
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		return Result{}, fmt.Errorf("OCR 返回格式不是有效 JSON")
	}
	text := strings.TrimSpace(collectText(result))
	if text == "" {
		return Result{Raw: result}, fmt.Errorf("OCR 未识别到文字")
	}
	return Result{Text: text, Raw: result}, nil
}

// ensureProcessLocked 确保 OCR 常驻进程已启动。
// 调用前必须持有锁。
func (e *Engine) ensureProcessLocked() error {
	if e.cmd != nil && e.cmd.Process != nil {
		if e.cmd.ProcessState == nil || !e.cmd.ProcessState.Exited() {
			return nil
		}
		e.stopLocked()
	}
	executable := e.executablePath()
	if executable == "" {
		return fmt.Errorf("OCR 组件未安装，请先安装 RapidOCR-json 运行组件")
	}
	args := ocrArgs()
	cmd := exec.Command(executable, args...)
	hideCommandWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建 OCR 输入管道失败：%w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 OCR 输出管道失败：%w", err)
	}
	logFile, logPath, err := openOCRLog(e.cfg.RuntimeDir)
	if err != nil {
		return err
	}
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("启动 OCR 组件失败：%w", err)
	}
	e.cmd = cmd
	e.stdin = stdin
	e.stdout = bufio.NewReader(stdout)
	e.logFile = logFile
	e.logPath = logPath
	e.done = make(chan error, 1)
	go func() {
		e.done <- cmd.Wait()
	}()
	return nil
}

// readJSONLineLocked 读取 OCR 常驻进程返回的一行 JSON。
// 调用前必须持有锁。
func (e *Engine) readJSONLineLocked(ctx context.Context) (string, error) {
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		for {
			line, err := e.stdout.ReadString('\n')
			if err != nil {
				errCh <- e.ocrReadError(err)
				return
			}
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}
			resultCh <- line
			return
		}
	}()
	select {
	case <-ctx.Done():
		if e.cmd != nil && e.cmd.Process != nil {
			_ = e.cmd.Process.Kill()
		}
		return "", fmt.Errorf("OCR 识别已取消")
	case err := <-errCh:
		return "", err
	case line := <-resultCh:
		return line, nil
	}
}

// stopLocked 停止 OCR 常驻进程。
// 调用前必须持有锁。
func (e *Engine) stopLocked() {
	if e.stdin != nil {
		_ = e.stdin.Close()
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_, _ = e.cmd.Process.Wait()
	}
	e.cmd = nil
	e.stdin = nil
	e.stdout = nil
	if e.logFile != nil {
		_ = e.logFile.Close()
	}
	e.logFile = nil
	e.logPath = ""
	e.done = nil
}

// openOCRLog 打开 OCR 组件日志文件。
// runtimeDir 为运行组件目录，返回日志文件和日志路径。
func openOCRLog(runtimeDir string) (*os.File, string, error) {
	logDir := filepath.Join(runtimeDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("创建 OCR 日志目录失败：%w", err)
	}
	logPath := filepath.Join(logDir, "ocr.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("打开 OCR 日志失败：%w", err)
	}
	_, _ = fmt.Fprintf(file, "\n[%s] 启动 OCR 组件\n", time.Now().Format(time.RFC3339))
	return file, logPath, nil
}

// ocrReadError 格式化 OCR 读取失败错误。
// err 为 stdout 读取错误。
func (e *Engine) ocrReadError(err error) error {
	if err == nil {
		return nil
	}
	if e.done != nil {
		select {
		case exitErr := <-e.done:
			if exitErr != nil {
				return fmt.Errorf("OCR 组件已退出：%v，请查看日志：%s", exitErr, e.logPath)
			}
			return fmt.Errorf("OCR 组件已退出，请查看日志：%s", e.logPath)
		default:
		}
	}
	if err == io.EOF {
		return fmt.Errorf("OCR 组件没有返回结果并已关闭输出，请查看日志：%s", e.logPath)
	}
	return fmt.Errorf("读取 OCR 返回失败：%w，请查看日志：%s", err, e.logPath)
}

// executablePath 返回 OCR 可执行文件路径。
// 优先使用环境变量，其次使用运行目录和系统 PATH。
func (e *Engine) executablePath() string {
	candidates := []string{}
	if value := strings.TrimSpace(os.Getenv("GOODHR_OCR_EXECUTABLE")); value != "" {
		candidates = append(candidates, value)
	}
	names := ocrExecutableNames()
	for _, name := range names {
		candidates = append(candidates, filepath.Join(e.cfg.OCRDir, name))
	}
	for _, name := range names {
		if found, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, found)
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// ocrExecutableNames 返回当前系统可能的 OCR 可执行文件名。
func ocrExecutableNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"RapidOCR-json.exe", "RapidOCR_json.exe", "rapidocr-json.exe"}
	}
	return []string{"RapidOCR-json", "RapidOCR_json", "rapidocr-json"}
}

// ocrArgs 返回 OCR 启动参数。
// 可通过 GOODHR_OCR_ARGS 传入额外参数。
func ocrArgs() []string {
	raw := strings.TrimSpace(os.Getenv("GOODHR_OCR_ARGS"))
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

// collectText 从 OCR JSON 中提取文字。
// value 为 OCR 原始 JSON。
func collectText(value any) string {
	lines := []string{}
	collectTextInto(value, "", &lines)
	return strings.Join(lines, "\n")
}

// collectTextInto 递归收集 OCR 文字字段。
// value 为当前 JSON 值，key 为字段名，lines 为结果列表。
func collectTextInto(value any, key string, lines *[]string) {
	switch item := value.(type) {
	case map[string]any:
		for childKey, childValue := range item {
			collectTextInto(childValue, childKey, lines)
		}
	case []any:
		for _, child := range item {
			collectTextInto(child, key, lines)
		}
	case string:
		text := strings.TrimSpace(item)
		lowerKey := strings.ToLower(key)
		if text != "" && (lowerKey == "text" || lowerKey == "txt" || lowerKey == "label" || lowerKey == "data" || lowerKey == "result") {
			*lines = append(*lines, text)
		}
	}
}
