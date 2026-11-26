// Package sessionfile 文件作用：在当前用户私有目录原子保存云端会话，并按 API 与命令通道地址隔离恢复。
package sessionfile

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const maxSessionBytes = 8192

var errSessionFile = errors.New("本地登录记录暂时不可用，请重新连接 GoodHR")

// record 只保存已验证凭证及其两个云端目标，不包含密码、用户资料或新的有效期。
type record struct {
	CloudURL string `json:"cloud_url"`
	WSURL    string `json:"ws_url"`
	Token    string `json:"token"`
}

// endpointKey 规范化协议、主机、默认端口和路径末尾；拒绝带身份或查询参数的地址。
func endpointKey(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errSessionFile
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", errSessionFile
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Path, u.RawPath = strings.TrimRight(u.Path, "/"), ""
	return u.String(), nil
}

// sessionPath 只允许真实私有子目录和普通文件；拒绝符号链接，避免覆盖其他用户文件。
func sessionPath(dataDir string, create bool) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", errSessionFile
	}
	directory := filepath.Join(dataDir, "cloud-session")
	if create {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return "", errSessionFile
		}
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || protectPath(directory, true) != nil {
		return "", errSessionFile
	}
	path := filepath.Join(directory, "session.json")
	info, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil || !info.Mode().IsRegular() || protectPath(path, false) != nil {
		return "", errSessionFile
	}
	return path, nil
}

// Load 仅恢复与当前 API 和 WS 目标同时匹配的凭证；不联网，也不延长服务器有效期。
func Load(dataDir, cloudURL, wsURL string) (string, error) {
	apiKey, err := endpointKey(cloudURL)
	if err != nil {
		return "", err
	}
	if wsURL == "" {
		wsURL = cloudURL
	}
	wsKey, err := endpointKey(wsURL)
	if err != nil {
		return "", err
	}
	path, err := sessionPath(dataDir, false)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", errSessionFile
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", errSessionFile
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxSessionBytes+1))
	var saved record
	if err != nil || len(body) > maxSessionBytes || json.Unmarshal(body, &saved) != nil {
		return "", errSessionFile
	}
	if saved.CloudURL != apiKey || saved.WSURL != wsKey {
		return "", nil
	}
	return strings.TrimSpace(saved.Token), nil
}

// Save 按现有同目录临时文件加 Rename 模式替换完整记录，失败不截断旧会话。
// macOS 使用 0700/0600；Windows 使用当前用户 SID 的独占 DACL，替换失败不先删除旧文件。
func Save(dataDir, cloudURL, wsURL, token string) error {
	apiKey, err := endpointKey(cloudURL)
	if err != nil {
		return err
	}
	if wsURL == "" {
		wsURL = cloudURL
	}
	wsKey, err := endpointKey(wsURL)
	if err != nil || strings.TrimSpace(token) == "" {
		return errSessionFile
	}
	body, err := json.Marshal(record{CloudURL: apiKey, WSURL: wsKey, Token: token})
	if err != nil || len(body) > maxSessionBytes {
		return errSessionFile
	}
	path, err := sessionPath(dataDir, true)
	if err != nil {
		return errSessionFile
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return errSessionFile
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if protectPath(temporary.Name(), false) != nil {
		return errSessionFile
	}
	if _, err = temporary.Write(body); err != nil {
		return errSessionFile
	}
	if temporary.Sync() != nil || temporary.Close() != nil {
		return errSessionFile
	}
	if os.Rename(temporary.Name(), path) != nil {
		return errSessionFile
	}
	return nil
}

// Delete 删除当前私有会话文件；不存在视为已退出，其他错误必须交给调用方处理。
func Delete(dataDir string) error {
	path, err := sessionPath(dataDir, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errSessionFile
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errSessionFile
	}
	return nil
}
