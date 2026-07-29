// Package console 文件作用：等待本地服务就绪后，用 macOS 默认浏览器打开云端控制台。
package console

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ExistingAgent 检查固定端口上的服务是否为已经运行的 GoodHR 本地程序。
func ExistingAgent(ctx context.Context, healthURL string, expectedPort int) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Status  string `json:"status"`
			Version string `json:"version"`
			Port    int    `json:"port"`
			DataDir string `json:"dataDir"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return false
	}
	return payload.OK &&
		payload.Data.Status == "ok" &&
		strings.TrimSpace(payload.Data.Version) != "" &&
		payload.Data.Port == expectedPort &&
		strings.TrimSpace(payload.Data.DataDir) != ""
}

// OpenWhenReady 等待健康检查成功，并打开带本地端口的控制台地址。
func OpenWhenReady(ctx context.Context, healthURL string, consoleURL string, localPort int) error {
	if err := waitReady(ctx, healthURL, 6*time.Second); err != nil {
		return err
	}
	target, err := withLocalPort(consoleURL, localPort)
	if err != nil {
		return err
	}
	if err = openBrowserURL(ctx, target); err != nil {
		return fmt.Errorf("打开 GoodHR 控制台失败：%w", err)
	}
	return nil
}

// waitReady 轮询本地健康接口直到成功或超时。
func waitReady(ctx context.Context, healthURL string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 800 * time.Millisecond}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(waitCtx, http.MethodGet, healthURL, nil)
		if err != nil {
			return err
		}
		response, requestErr := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		if requestErr == nil && response != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("本地服务还没准备好：%w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// withLocalPort 给控制台地址追加本地程序实际端口。
func withLocalPort(rawURL string, localPort int) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("控制台地址不正确")
	}
	if localPort <= 0 || localPort > 65535 {
		return "", fmt.Errorf("本地程序端口不正确")
	}
	query := parsed.Query()
	query.Set("local_port", strconv.Itoa(localPort))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
