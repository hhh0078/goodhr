// 本文件只升级逐字等于旧内置值的自动回复提示词，保留管理员真正自定义的文本。
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const legacyAutoReplyDialogueDigest = "3e80fe83652046613894d27be490728db4cf81d910dbcb58a9edf7c990d323cf"
const previousAutoReplyDialogueDigest = "78039b823ce91f2beca06ec5b622a1e4d3b766f890f42d3fc46abae2251a40ea"
const legacyAutoReplyReviewDigest = "6175ebc13ab684792050c10e0165f37d82d4612a0a629e010d58d305ae9c10bc"

// upgradeLegacyAutoReplyPrompts 识别旧默认值的固定摘要；自定义内容即使相似也不覆盖。
func upgradeLegacyAutoReplyPrompts(prompts *DefaultPrompts) {
	for _, field := range []struct {
		value   *string
		digest  string
		current string
	}{{&prompts.Dialogue, legacyAutoReplyDialogueDigest, builtinAutoReplyPrompts.Dialogue}, {&prompts.Review, legacyAutoReplyReviewDigest, builtinAutoReplyPrompts.Review}} {
		sum := sha256.Sum256([]byte(strings.TrimSpace(*field.value)))
		if digest := hex.EncodeToString(sum[:]); digest == field.digest || field.value == &prompts.Dialogue && digest == previousAutoReplyDialogueDigest {
			*field.value = field.current
		}
	}
}
