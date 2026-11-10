// Package httpapi 本文件校验内置 AI 的真实域名，阻止自定义接口通过路径、查询参数或相似域名绕过岗位启动检查。
package httpapi

import (
	"net/url"
	"strings"
)

const builtinAIRequiredMessage = "现在已不支持自定义 AI 接口，具体说明请前往「个人配置」查看与修改。"

// isOfficialAIURL 只接受 HTTP(S) 下的 58it.cn 及其子域名，不接受用户信息或字符串伪装。
func isOfficialAIURL(address string) bool {
	endpoint, err := url.Parse(strings.TrimSpace(address))
	if err != nil || endpoint.User != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return false
	}
	host := strings.ToLower(endpoint.Hostname())
	return host == "58it.cn" || strings.HasSuffix(host, ".58it.cn")
}
