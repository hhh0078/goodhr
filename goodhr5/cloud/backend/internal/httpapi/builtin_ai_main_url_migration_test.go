// 本文件使用独立 PostgreSQL 测试库验证 AI 地址恢复主站，仅允许目标地址变化并保留其余完整配置。
package httpapi

import (
	"fmt"
	"testing"
	"time"
)

// TestRestoreBuiltinAIMainURLPostgresMigration 验证中转地址批量恢复主站时保留全部其他字段、第三方配置且可重复执行。
func TestRestoreBuiltinAIMainURLPostgresMigration(t *testing.T) {
	db := aiRechargePostgresTestDB(t, true)
	if _, err := db.Exec(`
		CREATE TABLE user_ai_configs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL UNIQUE REFERENCES users(id),
			base_url TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			api_key_encrypted TEXT NOT NULL DEFAULT '',
			temperature NUMERIC(3, 2),
			prompt_template TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		COMMENT ON TABLE user_ai_configs IS 'AI 地址恢复主站的测试配置';
		COMMENT ON COLUMN user_ai_configs.id IS '测试配置主键';
		COMMENT ON COLUMN user_ai_configs.user_id IS '测试用户主键';
		COMMENT ON COLUMN user_ai_configs.base_url IS '测试 AI 接口地址';
		COMMENT ON COLUMN user_ai_configs.model IS '测试模型';
		COMMENT ON COLUMN user_ai_configs.api_key_encrypted IS '合成测试密钥';
		COMMENT ON COLUMN user_ai_configs.temperature IS '测试温度';
		COMMENT ON COLUMN user_ai_configs.prompt_template IS '测试提示词';
		COMMENT ON COLUMN user_ai_configs.enabled IS '测试启用状态';
		COMMENT ON COLUMN user_ai_configs.created_at IS '测试创建时间';
		COMMENT ON COLUMN user_ai_configs.updated_at IS '测试更新时间';
	`); err != nil {
		t.Fatal(err)
	}
	const migration = "0128_restore_builtin_ai_main_url.sql"
	const transitURL = "https://goodhr.transit.ai.58it.cn/api/ai-compatible/v1/chat/completions"
	const mainURL = "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions"
	const externalURL = "https://custom.example.com/v1/chat/completions"
	seedTime := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	users := []struct {
		address, key, want string
		id, snapshot       string
	}{
		{address: transitURL, key: "ghai_synthetic", want: mainURL},
		{address: "  HTTPS://GOODHR.TRANSIT.AI.58IT.CN/api/ai-compatible/v1/chat/completions  ", key: "custom-synthetic", want: mainURL},
		{address: transitURL, want: mainURL},
		{address: externalURL, key: "ghai_external", want: externalURL},
		{address: mainURL, key: "ghai_existing", want: mainURL},
		{address: "https://goodhr.transit.ai.58it.cn.external.test/api/ai-compatible/v1/chat/completions", key: "ghai_lookalike", want: "https://goodhr.transit.ai.58it.cn.external.test/api/ai-compatible/v1/chat/completions"},
		{address: "https://goodhr.transit.ai.58it.cn/other-route", key: "ghai_other", want: "https://goodhr.transit.ai.58it.cn/other-route"},
		{address: "", want: ""},
	}
	for index := range users {
		item := &users[index]
		if err := db.QueryRow(`INSERT INTO users(email) VALUES($1) RETURNING id::text`, fmt.Sprintf("restore-main-%d@example.test", index)).Scan(&item.id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO user_ai_configs(user_id,base_url,model,api_key_encrypted,temperature,prompt_template,enabled,created_at,updated_at) VALUES($1::uuid,$2,'keep-model',$3,0.35,'保留原提示词',$4,$5,$5)`, item.id, item.address, item.key, index%2 == 0, seedTime); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT (to_jsonb(ai)-'base_url')::text FROM user_ai_configs ai WHERE user_id=$1::uuid`, item.id).Scan(&item.snapshot); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		aiRechargeApplyMigration(t, db, migration)
		for index, item := range users {
			var address, snapshot string
			if err := db.QueryRow(`SELECT base_url,(to_jsonb(ai)-'base_url')::text FROM user_ai_configs ai WHERE user_id=$1::uuid`, item.id).Scan(&address, &snapshot); err != nil {
				t.Fatal(err)
			}
			if address != item.want || snapshot != item.snapshot {
				t.Fatalf("个人配置第 %d 项第 %d 次迁移改变了地址以外字段或匹配了错误地址", index, attempt+1)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO system_configs(config_key,config_value,description,enabled,created_at,updated_at) VALUES('system.app_config','{}'::jsonb,'保留系统说明',false,$1,$1)`, seedTime); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, before, want string }{
		{"中转地址", `{"builtin_ai":{"public_base_url":"` + transitURL + `","default_model":"keep-model","upstream_api_key":"synthetic-upstream","models":[{"id":"keep-model"}]},"keep":{"x":1}}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `","default_model":"keep-model","upstream_api_key":"synthetic-upstream","models":[{"id":"keep-model"}]},"keep":{"x":1}}`},
		{"空白与大小写", `{"builtin_ai":{"public_base_url":"  HTTPS://GOODHR.TRANSIT.AI.58IT.CN/api/ai-compatible/v1/chat/completions  "}}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `"}}`},
		{"空地址", `{"builtin_ai":{"public_base_url":"  ","default_model":"keep-model"}}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `","default_model":"keep-model"}}`},
		{"地址为null", `{"builtin_ai":{"public_base_url":null,"default_model":"keep-model"}}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `","default_model":"keep-model"}}`},
		{"缺少地址", `{"builtin_ai":{"default_model":"keep-model"}}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `","default_model":"keep-model"}}`},
		{"缺少内置配置", `{"keep":1}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `"},"keep":1}`},
		{"内置配置为null", `{"builtin_ai":null,"keep":1}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `"},"keep":1}`},
		{"第三方地址", `{"builtin_ai":{"public_base_url":"` + externalURL + `"}}`, `{"builtin_ai":{"public_base_url":"` + externalURL + `"}}`},
		{"当前主站地址", `{"builtin_ai":{"public_base_url":"` + mainURL + `"}}`, `{"builtin_ai":{"public_base_url":"` + mainURL + `"}}`},
		{"无效根配置", `null`, `null`},
		{"无效内置配置", `{"builtin_ai":[],"keep":1}`, `{"builtin_ai":[],"keep":1}`},
	} {
		t.Run(item.name, func(t *testing.T) {
			if _, err := db.Exec(`UPDATE system_configs SET config_value=$1::jsonb WHERE config_key='system.app_config'`, item.before); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := db.QueryRow(`SELECT to_jsonb(s)::text FROM system_configs s WHERE config_key='system.app_config'`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				aiRechargeApplyMigration(t, db, migration)
				var matches bool
				if err := db.QueryRow(`SELECT to_jsonb(s)=($1::jsonb || jsonb_build_object('config_value',$2::jsonb)) FROM system_configs s WHERE config_key='system.app_config'`, before, item.want).Scan(&matches); err != nil {
					t.Fatal(err)
				}
				if !matches {
					t.Fatalf("系统配置第 %d 次迁移未保留预期地址与其余完整字段", attempt+1)
				}
			}
		})
	}
}
