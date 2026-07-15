//go:build windows

// 本文件用于验证 Windows 固定端口占用进程的解析和安全识别逻辑。
package process

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestParseListeningPID 验证 IPv4 和 IPv6 监听记录都能正确识别。
func TestParseListeningPID(t *testing.T) {
	output := "  TCP    127.0.0.1:55271      0.0.0.0:0      LISTENING       824\r\n" +
		"  TCP    [::1]:55272          [::]:0         LISTENING       25816\r\n"
	pid, occupied, err := parseListeningPID(output, 55271)
	if err != nil || !occupied || pid != 824 {
		t.Fatalf("解析 IPv4 监听记录失败：pid=%d occupied=%v err=%v", pid, occupied, err)
	}
	pid, occupied, err = parseListeningPID(output, 55272)
	if err != nil || !occupied || pid != 25816 {
		t.Fatalf("解析 IPv6 监听记录失败：pid=%d occupied=%v err=%v", pid, occupied, err)
	}
}

// TestParseListeningPIDIgnoresOtherStates 验证非监听连接和其他端口不会被误判。
func TestParseListeningPIDIgnoresOtherStates(t *testing.T) {
	output := "  TCP    127.0.0.1:55271      127.0.0.1:51000      ESTABLISHED       999\r\n"
	pid, occupied, err := parseListeningPID(output, 55271)
	if err != nil || occupied || pid != 0 {
		t.Fatalf("非监听连接被误判：pid=%d occupied=%v err=%v", pid, occupied, err)
	}
}

// TestVerifyGoodHRHealth 验证只有包含完整 GoodHR 身份字段的健康接口才会通过。
func TestVerifyGoodHRHealth(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantErr    bool
		portMarker string
	}{
		{name: "GoodHR 健康接口", body: `{"ok":true,"data":{"status":"ok","version":"5.3.2","port":PORT,"dataDir":"D:/GoodHR/data"}}`, portMarker: "PORT"},
		{name: "其他软件接口", body: `{"ok":true,"data":{"status":"ok"}}`, wantErr: true},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, item.body)
			}))
			defer server.Close()
			host, rawPort, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatalf("解析测试服务器地址失败：%v", err)
			}
			port, err := strconv.Atoi(rawPort)
			if err != nil {
				t.Fatalf("解析测试服务器端口失败：%v", err)
			}
			if item.portMarker != "" {
				item.body = strings.ReplaceAll(item.body, item.portMarker, strconv.Itoa(port))
			}
			err = verifyGoodHRHealth(host, port)
			if (err != nil) != item.wantErr {
				t.Fatalf("健康接口识别结果不符合预期：err=%v wantErr=%v", err, item.wantErr)
			}
		})
	}
}
