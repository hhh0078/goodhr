// 文件作用说明：统一云端个人配置与本地浏览器协议使用的鼠标速度档位和默认值。
package contract

import "strings"

// NormalizeMouseSpeed 把旧版空值或未知配置归一为更快的默认档位。
func NormalizeMouseSpeed(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "ultra_fast", "fast", "standard", "relaxed", "slow":
		return value
	default:
		return "fast"
	}
}
