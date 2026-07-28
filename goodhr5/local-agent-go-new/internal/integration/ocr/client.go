// Package ocr 调用本机 OCR 可执行文件识别截图文字，并保持敏感内容只在本地。
package ocr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Client 管理 OCR 常驻子进程和 JSON 行协议。
type Client struct {
	executable string
	mu         sync.Mutex
}

// Result 表示 OCR 文字识别结果。
type Result struct {
	Text string `json:"text"`
}

// New 创建 OCR 客户端。
func New(executable string) *Client {
	return &Client{executable: strings.TrimSpace(executable)}
}

// Ready 检查 OCR 可执行文件是否存在。
func (c *Client) Ready() error {
	path, err := c.resolveExecutable()
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("OCR 组件暂不可用")
	}
	return nil
}

// Recognize 启动一次本地 OCR 进程并识别绝对路径图片。
func (c *Client) Recognize(ctx context.Context, imagePath string) (Result, error) {
	if !filepath.IsAbs(imagePath) {
		return Result{}, fmt.Errorf("OCR 图片路径必须是绝对路径")
	}
	if _, err := os.Stat(imagePath); err != nil {
		return Result{}, fmt.Errorf("OCR 图片不存在：%w", err)
	}
	executable, err := c.resolveExecutable()
	if err != nil {
		return Result{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	command := exec.CommandContext(ctx, executable)
	stdin, err := command.StdinPipe()
	if err != nil {
		return Result{}, fmt.Errorf("创建 OCR 输入失败：%w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("创建 OCR 输出失败：%w", err)
	}
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("启动 OCR 失败：%w", err)
	}
	if err := json.NewEncoder(stdin).Encode(struct {
		ImagePath string `json:"image_path"`
	}{ImagePath: imagePath}); err != nil {
		_ = command.Process.Kill()
		return Result{}, fmt.Errorf("发送 OCR 请求失败：%w", err)
	}
	_ = stdin.Close()
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		_ = command.Process.Kill()
		return Result{}, fmt.Errorf("读取 OCR 结果失败：%w", err)
	}
	var result Result
	if err := json.Unmarshal(line, &result); err != nil {
		_ = command.Process.Kill()
		return Result{}, fmt.Errorf("解析 OCR 结果失败：%w", err)
	}
	if err := command.Wait(); err != nil {
		return Result{}, fmt.Errorf("OCR 进程执行失败：%w", err)
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" {
		return Result{}, fmt.Errorf("OCR 没识别到文字")
	}
	return result, nil
}

// resolveExecutable 按配置和 PATH 查找 OCR 可执行文件。
func (c *Client) resolveExecutable() (string, error) {
	if c.executable != "" {
		return c.executable, nil
	}
	for _, name := range []string{"RapidOCR-json", "rapidocr-json"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("OCR 组件还没安装")
}
