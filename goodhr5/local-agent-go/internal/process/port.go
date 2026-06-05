// Package process 负责端口探测和本地进程辅助管理。
package process

import (
	"fmt"
	"net"
)

// ListenFirstAvailable 从起始端口到结束端口查找可监听端口。
// host 为监听地址，start 为起始端口，end 为结束端口，返回监听器和实际端口。
func ListenFirstAvailable(host string, start int, end int) (net.Listener, int, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if start <= 0 {
		start = 9001
	}
	if end < start {
		end = start
	}
	var lastErr error
	for port := start; port <= end; port++ {
		addr := fmt.Sprintf("%s:%d", host, port)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, port, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("没有可用端口：%d-%d，最后错误：%w", start, end, lastErr)
}
