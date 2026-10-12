// Package ocr 管理本机 OCR 常驻进程，按 JSON 行协议识别截图并保持敏感内容只在本地。
package ocr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// ErrorUnavailable 表示 OCR 组件未安装、无法启动或异常退出。
	ErrorUnavailable = "OCR_UNAVAILABLE"
	// ErrorNoText 表示当前图片没有识别到文字。
	ErrorNoText = "OCR_NO_TEXT"
)

// Error 表示 OCR 稳定错误码和原始原因。
type Error struct {
	Code    string
	Message string
	Cause   error
}

// Error 返回适合本地日志展示的 OCR 错误文本。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s：%v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap 返回底层 OCR 错误。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsUnavailable 判断 OCR 组件是否已经无法继续服务当前任务。
func IsUnavailable(err error) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == ErrorUnavailable
}

// IsNoText 判断当前图片是否只是没有识别到文字。
func IsNoText(err error) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == ErrorNoText
}

// Client 管理 OCR 常驻子进程和串行 JSON 行请求。
type Client struct {
	executable string
	mu         sync.Mutex
	command    *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	done       chan error
	processLog *ocrProcessDiagnostics
	logSink    func([]byte)
}

// Result 表示 OCR 文字识别结果。
type Result struct {
	Text string `json:"text"`
}

// New 创建 OCR 客户端。
func New(executable string, logSinks ...func([]byte)) *Client {
	client := &Client{executable: strings.TrimSpace(executable)}
	if len(logSinks) > 0 {
		client.logSink = logSinks[0]
	}
	return client
}

// Ready 检查 OCR 可执行文件；Windows 额外检查 RapidOCR 模型，macOS 使用系统 Vision。
func (c *Client) Ready() error {
	path, err := c.resolveExecutable()
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return &Error{Code: ErrorUnavailable, Message: "OCR 组件暂不可用", Cause: err}
	}
	if runtime.GOOS == "darwin" {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return &Error{Code: ErrorUnavailable, Message: "OCR 组件没有执行权限"}
		}
		return nil
	}
	modelPath := filepath.Join(filepath.Dir(path), "models", "ch_PP-OCRv3_det_infer.onnx")
	if _, err = os.Stat(modelPath); err != nil {
		return &Error{Code: ErrorUnavailable, Message: "OCR 模型文件不完整", Cause: err}
	}
	return nil
}

// Recognize 通过常驻 OCR 进程识别一张绝对路径图片。
func (c *Client) Recognize(ctx context.Context, imagePath string) (Result, error) {
	imagePath = strings.TrimSpace(imagePath)
	if !filepath.IsAbs(imagePath) {
		return Result{}, fmt.Errorf("OCR 图片路径必须是绝对路径")
	}
	if _, err := os.Stat(imagePath); err != nil {
		return Result{}, fmt.Errorf("OCR 图片不存在：%w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 1; attempt <= 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if err := c.ensureProcessLocked(); err != nil {
			return Result{}, err
		}
		pid := c.command.Process.Pid
		err := json.NewEncoder(c.stdin).Encode(struct {
			ImagePath string `json:"image_path"`
		}{ImagePath: imagePath})
		var line []byte
		if err == nil {
			line, err = c.readJSONLineLocked(ctx)
		}
		if err == nil {
			text, parseErr := parseOCRText(line)
			return Result{Text: text}, parseErr
		}
		log.Printf("event=ocr_process_failed pid=%d attempt=%d error=%q", pid, attempt, err)
		if ctx.Err() == nil {
			c.processLog.mark("", err)
		}
		c.stopLocked()
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if attempt == 2 {
			return Result{}, &Error{Code: ErrorUnavailable, Message: "OCR 组件重启后仍未返回结果", Cause: err}
		}
		log.Printf("event=ocr_process_retry attempt=%d", attempt+1)
	}
	return Result{}, &Error{Code: ErrorUnavailable, Message: "OCR 组件没有返回结果"}
}

// Close 停止 OCR 常驻进程并释放管道。
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
}

// ensureProcessLocked 确保 OCR 常驻进程已经启动。
func (c *Client) ensureProcessLocked() error {
	if c.command != nil && c.command.Process != nil {
		select {
		case exitErr := <-c.done:
			log.Printf("event=ocr_process_exited pid=%d error=%v", c.command.Process.Pid, exitErr)
			c.done = nil
		default:
			return nil
		}
	}
	c.stopLocked()
	executable, err := c.resolveExecutable()
	if err != nil {
		return err
	}
	if err = c.Ready(); err != nil {
		return err
	}
	command := exec.Command(executable, ocrArgs()...)
	processLog := &ocrProcessDiagnostics{startedAt: time.Now()}
	command.Stderr = processLog
	configureCommand(command)
	command.Dir = filepath.Dir(executable)
	stdin, err := command.StdinPipe()
	if err != nil {
		return &Error{Code: ErrorUnavailable, Message: "创建 OCR 输入失败", Cause: err}
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return &Error{Code: ErrorUnavailable, Message: "创建 OCR 输出失败", Cause: err}
	}
	if err = command.Start(); err != nil {
		_ = stdin.Close()
		return &Error{Code: ErrorUnavailable, Message: "启动 OCR 失败", Cause: err}
	}
	c.command = command
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.done = make(chan error, 1)
	c.processLog = processLog
	done := c.done
	go func() {
		err := command.Wait()
		if c.logSink != nil {
			c.logSink(processLog.exitEvent(command.Process.Pid, command.ProcessState.ExitCode(), err))
		}
		done <- err
	}()
	log.Printf("event=ocr_process_started pid=%d", command.Process.Pid)
	return nil
}

// readJSONLineLocked 读取 OCR 返回的下一行 JSON，并响应任务取消。
func (c *Client) readJSONLineLocked(ctx context.Context) ([]byte, error) {
	result := make(chan []byte, 1)
	failed := make(chan error, 1)
	reader := c.stdout
	done := c.done
	go func() {
		for {
			line, err := reader.ReadBytes('\n')
			line = bytes.TrimSpace(line)
			if len(line) > 0 && json.Valid(line) {
				result <- line
				return
			}
			if err != nil {
				failed <- err
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		c.processLog.mark("OCR 请求取消："+ctx.Err().Error(), nil)
		if c.command != nil && c.command.Process != nil {
			_ = c.command.Process.Kill()
		}
		return nil, &Error{Code: ErrorUnavailable, Message: "OCR 识别已取消", Cause: ctx.Err()}
	case line := <-result:
		return line, nil
	case err := <-failed:
		if done != nil {
			select {
			case exitErr := <-done:
				c.done = nil
				if exitErr != nil {
					err = exitErr
				}
			case <-time.After(250 * time.Millisecond):
			}
		}
		return nil, &Error{Code: ErrorUnavailable, Message: "OCR 组件没有返回结果", Cause: err}
	}
}

// stopLocked 停止当前 OCR 进程并清空进程引用。
func (c *Client) stopLocked() {
	c.processLog.mark("主程序主动回收 OCR 进程", nil)
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.command != nil && c.command.Process != nil {
		_ = c.command.Process.Kill()
	}
	if c.done != nil {
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
		}
	}
	c.command = nil
	c.stdin = nil
	c.stdout = nil
	c.done = nil
	c.processLog = nil
}

// resolveExecutable 按配置和 PATH 查找 OCR 可执行文件。
func (c *Client) resolveExecutable() (string, error) {
	if c.executable != "" {
		if info, err := os.Stat(c.executable); err == nil && !info.IsDir() {
			return c.executable, nil
		}
		if found := findOCRExecutable(filepath.Dir(c.executable)); found != "" {
			return found, nil
		}
	}
	for _, name := range []string{"RapidOCR-json", "RapidOCR_json", "rapidocr-json"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", &Error{Code: ErrorUnavailable, Message: "OCR 组件还没安装"}
}

// findOCRExecutable 在 OCR 安装根目录中查找可执行文件，返回空字符串表示没有找到。
func findOCRExecutable(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	names := map[string]bool{
		"rapidocr-json": true,
		"rapidocr_json": true,
	}
	found := ""
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !entry.IsDir() && names[strings.TrimSuffix(strings.ToLower(entry.Name()), ".exe")] {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// ocrArgs 从环境变量读取 OCR 组件附加启动参数。
func ocrArgs() []string {
	return strings.Fields(strings.TrimSpace(os.Getenv("GOODHR_OCR_ARGS")))
}

// parseOCRText 从 RapidOCR 顶层或嵌套 JSON 字段中提取文字。
func parseOCRText(raw json.RawMessage) (string, error) {
	if !json.Valid(raw) {
		return "", &Error{Code: ErrorUnavailable, Message: "OCR 返回格式不是有效 JSON"}
	}
	lines := make([]string, 0)
	collectOCRText(raw, "", &lines)
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return "", &Error{Code: ErrorNoText, Message: "OCR 没识别到文字"}
	}
	return text, nil
}

// collectOCRText 递归读取允许的 OCR 文字字段，忽略坐标、分数和诊断内容。
func collectOCRText(raw json.RawMessage, key string, lines *[]string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}
	switch raw[0] {
	case '{':
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return
		}
		for childKey, child := range object {
			collectOCRText(child, childKey, lines)
		}
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return
		}
		for _, item := range items {
			collectOCRText(item, key, lines)
		}
	case '"':
		if !isOCRTextKey(key) {
			return
		}
		var value string
		if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			*lines = append(*lines, strings.TrimSpace(value))
		}
	}
}

// isOCRTextKey 判断字段是否属于 RapidOCR 可识别的文字结果。
func isOCRTextKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "text", "txt", "label", "data", "result":
		return true
	default:
		return false
	}
}
