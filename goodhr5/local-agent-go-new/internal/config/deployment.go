// 本文件统一私有版客户端的服务地址和下载来源校验，Windows、Apple 与 Intel 共用相同策略。
package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// DefaultDeploymentMode 在私有打包时设为 private，普通版本保持原有 HTTPS 下载要求。
var DefaultDeploymentMode = "public"

// PrivateDeployment 判断本地程序是否为私有部署包。
func PrivateDeployment() bool { return DefaultDeploymentMode == "private" }

// ServiceOrigin 返回实际配置的服务源，环境变量仍支持部署者覆盖打包地址。
func ServiceOrigin() string {
	value := strings.TrimSpace(os.Getenv("GOODHR_CLOUD_API_BASE"))
	if value == "" {
		value = DefaultCloudURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return serviceURLOrigin(parsed)
}

// serviceURLOrigin 统一域名大小写和默认端口，与浏览器实际发送的 Origin 保持一致。
func serviceURLOrigin(parsed *url.URL) string {
	host := strings.ToLower(parsed.Host)
	if parsed.Scheme == "https" && parsed.Port() == "443" {
		host = strings.TrimSuffix(host, ":443")
	}
	if parsed.Scheme == "http" && parsed.Port() == "80" {
		host = strings.TrimSuffix(host, ":80")
	}
	return parsed.Scheme + "://" + host
}

// ValidateDownloadURL 私有版仅允许配置服务同源下载，普通版本继续只接受 HTTPS。
func ValidateDownloadURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("下载地址不正确")
	}
	if PrivateDeployment() {
		if ServiceOrigin() == "" || serviceURLOrigin(parsed) != ServiceOrigin() {
			return fmt.Errorf("私有版组件只能从配置的内网服务下载")
		}
	} else if parsed.Scheme != "https" {
		return fmt.Errorf("只支持 HTTPS 地址")
	}
	return nil
}

// CheckDownloadRedirect 限制私有下载每一跳同源，普通下载禁止 HTTPS 降级。
func CheckDownloadRedirect(next *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("下载跳转次数过多")
	}
	return ValidateDownloadURL(next.URL.String())
}
