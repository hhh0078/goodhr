// Package main 是 GoodHR 控制台 Wails 桌面壳入口。
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// main 启动 Wails 桌面窗口并代理本地控制台。
func main() {
	targetURL := consoleURLFromArgs()
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Fatalf("控制台地址不正确：%s", targetURL)
	}
	err = wails.Run(&options.App{
		Title:  "GoodHR 控制台",
		Width:  1280,
		Height: 860,
		AssetServer: &assetserver.Options{
			Handler: proxyConsole(target),
		},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 18, A: 1},
	})
	if err != nil {
		log.Fatalf("GoodHR 控制台启动失败：%v", err)
	}
}

// consoleURLFromArgs 从启动参数读取本地控制台地址。
// 未传入时默认使用 9001。
func consoleURLFromArgs() string {
	for _, arg := range os.Args[1:] {
		value := strings.TrimSpace(arg)
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			value = strings.TrimRight(value, "/")
			if strings.HasSuffix(value, "/admin") {
				return value + "/"
			}
			return value + "/admin/"
		}
	}
	return "http://127.0.0.1:9001/admin/"
}

// proxyConsole 创建控制台反向代理。
// target 为 Local Agent 控制台地址。
func proxyConsole(target *url.URL) http.Handler {
	proxyTarget := *target
	basePath := strings.TrimRight(proxyTarget.Path, "/")
	if basePath == "" {
		basePath = "/admin"
	}
	proxyTarget.Path = ""
	proxy := httputil.NewSingleHostReverseProxy(&proxyTarget)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.Header.Set("X-GoodHR-Shell", "wails")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Content-Security-Policy")
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			r.URL.Path = basePath + "/"
		}
		proxy.ServeHTTP(w, r)
	})
}
