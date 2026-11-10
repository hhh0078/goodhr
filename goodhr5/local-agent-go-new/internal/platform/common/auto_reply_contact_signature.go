// Package common 本文件复用全部联系人列表的持久摘要算法，列表换位与头像签名变化不产生伪新消息。
package common

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// AutoReplyContactListSignature 仅对可见姓名、摘要、整行正文和独立时间生成固定指纹，不保存原文或采用列表序号。
func AutoReplyContactListSignature(name, preview, rowText, contactTime string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{name, preview, rowText, strings.TrimSpace(contactTime)}, "\x00"))))
}
