// 本文件验证旧内置自动回复提示词读取时升级，而管理员真正自定义的文本与其他提示词保持原样。
package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestLegacyAutoReplyDefaultsUpgradeWithoutOverwritingCustom 验证真实旧版本配置可用新目标协议，自定义改动不会被默默抹掉。
func TestLegacyAutoReplyDefaultsUpgradeWithoutOverwritingCustom(t *testing.T) {
	raw, err := os.ReadFile("testdata/auto_reply_prompts_legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, custom := range []bool{false, true} {
		var fields map[string]any
		if err = json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if custom {
			fields["auto_reply_dialogue_prompt"] = fields["auto_reply_dialogue_prompt"].(string) + "\n管理员自定义：回复更简短"
			fields["auto_reply_review_prompt"] = fields["auto_reply_review_prompt"].(string) + "\n管理员自定义：先看当前简历"
		}
		body, _ := json.Marshal(fields)
		store := NewMemorySystemConfigStore()
		if err = store.Save(SystemConfig{ConfigKey: defaultPromptsConfigKey, ConfigValue: string(body), Enabled: true}); err != nil {
			t.Fatal(err)
		}
		got, err := resolveDefaultPrompts(store)
		if err != nil {
			t.Fatal(err)
		}
		if got.Resume != "保持的简历规则" {
			t.Fatal("升级影响了其他提示词")
		}
		if custom {
			if got.Dialogue != fields["auto_reply_dialogue_prompt"] || got.Review != fields["auto_reply_review_prompt"] {
				t.Fatal("覆盖了管理员自定义内容")
			}
		} else if got.Dialogue != builtinAutoReplyPrompts.Dialogue || got.Review != builtinAutoReplyPrompts.Review || !strings.Contains(got.Dialogue, "editable_goal_ids") || !strings.Contains(got.Review, "new_evidence_followup_allowed") {
			t.Fatal("持久化旧内置规则没有升级为动态目标协议")
		}
	}
}
