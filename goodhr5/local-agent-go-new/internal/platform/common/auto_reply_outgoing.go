// Package common 本文件按平台明确配置生成最终出站正文，供服务器发送账本与设备草稿核验共用。
package common

import (
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// NormalizeAutoReplyOutgoingText 去除正文外边空白，仅为显式启用单行的平台把CRLF/LF逐个换为普通空格。
// cfg 决定平台行为；正文内部已有的空格、制表符和其它文字均保留，返回值用于发送前定稿而非模糊匹配回执。
func NormalizeAutoReplyOutgoingText(cfg model.Config, message string) string {
	message = strings.TrimSpace(message)
	if cfg.Behavior.AutoReplySingleLineMessages {
		message = strings.ReplaceAll(strings.ReplaceAll(message, "\r\n", "\n"), "\n", " ")
	}
	return message
}
