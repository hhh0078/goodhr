// 本文件将现有自动回复编排模块托管在普通后端进程内，复用已有私有日志卷，不新增容器或监听端口。
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/cloudruntime"
)

// prepareAutoReplyEnvironment 在构造后端前准备内部凭证和私有附件目录；显式外部执行地址保留原部署方式。
// logPath 为已建立的后台日志路径，返回是否应在当前进程启动自动回复模块。
func prepareAutoReplyEnvironment(logPath string) (bool, error) {
	if strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_RUNTIME_URL")) != "" {
		return false, nil
	}
	files := strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR"))
	if files == "" {
		var err error
		files, err = filepath.Abs(filepath.Join(filepath.Dir(logPath), "auto-reply", "files"))
		if err != nil {
			return false, err
		}
	}
	if !filepath.IsAbs(files) {
		return false, fmt.Errorf("自动回复附件目录必须使用绝对路径")
	}
	if err := os.MkdirAll(files, 0700); err != nil {
		return false, fmt.Errorf("自动回复私有目录未准备好：%w", err)
	}
	info, err := os.Lstat(files)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("自动回复私有目录无效")
	}
	if strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN")) == "" {
		var value [32]byte
		if _, err := rand.Read(value[:]); err != nil {
			return false, err
		}
		if err := os.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", hex.EncodeToString(value[:])); err != nil {
			return false, err
		}
	}
	if err := os.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", files); err != nil {
		return false, err
	}
	return true, nil
}

// autoReplyBackendURL 从后端监听地址推导仅用于本进程事务回调的回环地址，不接受候选人输入。
func autoReplyBackendURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("后端监听地址不完整：%w", err)
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// embeddedAutoReply 创建同进程执行模块；返回服务以便后端注册处理器并在退出时等待既有事务收尾。
func embeddedAutoReply(addr string) (*cloudruntime.Service, error) {
	backend, err := autoReplyBackendURL(addr)
	if err != nil {
		return nil, err
	}
	return cloudruntime.New(backend, os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN"), os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR"))
}
