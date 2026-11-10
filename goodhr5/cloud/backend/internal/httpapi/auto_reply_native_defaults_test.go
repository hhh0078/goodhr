// Package httpapi 本文件验证新资料动作规则对应管理员动态提示词、旧默认精确升级及微信默认迁移不覆盖管理员删改。
package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestNativeInfoPromptUpgradePreservesCustom 验证真实上一版默认能升级，定制正文不变且快照继续拿到当前管理员配置。
func TestNativeInfoPromptUpgradePreservesCustom(t *testing.T) {
	raw, err := os.ReadFile("testdata/auto_reply_prompts_pre_native.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, custom := range []bool{false, true} {
		var fields map[string]string
		if err = json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if custom {
			fields["auto_reply_dialogue_prompt"] += "\n管理员自定义：不要机械问候。"
		}
		body, _ := json.Marshal(fields)
		store := NewMemorySystemConfigStore()
		if err = store.Save(SystemConfig{ConfigKey: defaultPromptsConfigKey, ConfigValue: string(body), Enabled: true}); err != nil {
			t.Fatal(err)
		}
		prompts, err := loadAutoReplyPrompts(store)
		if err != nil {
			t.Fatal(err)
		}
		if custom {
			if prompts.Dialogue != fields["auto_reply_dialogue_prompt"] {
				t.Fatal("管理员提示词被覆盖")
			}
		} else if !strings.Contains(prompts.Dialogue, "native_info_requests") || !strings.Contains(prompts.Dialogue, "online_profile") || !strings.Contains(prompts.Dialogue, "info_requests.request_resume") {
			t.Fatal("旧默认没有对应新流程")
		}
	}
}

// TestWechatDefaultsMigrationKeepsCustomLibrary 只升级完整旧内置库，管理员清空、删改、定制微信均保持原样；SQL无结构变更。
func TestWechatDefaultsMigrationKeepsCustomLibrary(t *testing.T) {
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'; ALTER TABLE positions ADD COLUMN user_id UUID REFERENCES users(id)`); err != nil {
		t.Fatal(err)
	}
	applyAutoReplyTestMigrations(t, db)
	migration, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0121_auto_reply_wechat_defaults.sql"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostgresSystemConfigStore(db)
	initial, err := store.Get(autoReplyLibraryKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"builtin", "empty", "modified", "removed"} {
		t.Run(scenario, func(t *testing.T) {
			lib, err := parseAutoReplyLibrary(initial.ConfigValue)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "empty":
				lib.Phrases = []ReplyPhrase{}
			case "modified":
				lib.Phrases[0].Content = "管理员自己的开场"
			case "removed":
				lib.Phrases = slices.Delete(lib.Phrases, 0, 1)
			}
			raw, _ := json.Marshal(lib)
			record := initial
			record.ConfigValue = string(raw)
			if err = store.Save(record); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(string(migration)); err != nil {
				t.Fatal(err)
			}
			after, err := readAutoReplyLibrary(store)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "builtin" {
				if after.revision() != lib.revision() {
					t.Fatal("迁移覆盖了管理员选择")
				}
				return
			}
			ids := map[string]bool{}
			for _, phrase := range after.Phrases {
				ids[phrase.ID] = true
			}
			if len(after.Phrases) != 15 || !ids["request_wechat"] || !ids["receive_wechat"] || !ids["required_mismatch"] {
				t.Fatal("新默认场景未补齐")
			}
			if _, err = db.Exec(string(migration)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
