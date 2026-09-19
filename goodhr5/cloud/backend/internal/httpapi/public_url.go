// 本文件统一读取部署环境的公开域名，避免测试环境生成正式环境链接。
package httpapi

import (
	"os"
	"strings"
)

// deploymentPublicBaseURL 返回部署配置指定的域名；未配置时使用正式官网。
func deploymentPublicBaseURL() string {
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("GOODHR_PUBLIC_BASE_URL")), "/"); value != "" {
		return value
	}
	return "https://goodhr.58it.cn"
}

// environmentPublicURL 将历史 GoodHR 站内链接改为当前环境，保留外部自定义链接。
func environmentPublicURL(value string) string {
	for _, base := range []string{"https://goodhr5.58it.cn", "https://goodhr.ai.58it.cn", "https://goodhr.58it.cn", "https://goodhr-test.ai.58it.cn"} {
		if value == base || strings.HasPrefix(value, base+"/") {
			return deploymentPublicBaseURL() + strings.TrimPrefix(value, base)
		}
	}
	return value
}
