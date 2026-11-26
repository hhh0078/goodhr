// 本文件验证内置 AI 默认正式地址及历史地址迁移，不改动用户自定义的第三方接口。
package httpapi

import (
	"fmt"
	"testing"
	"time"
)

// TestBuiltinAIFallbackUsesProductionURL 验证正式环境默认使用官网 AI，测试和私有部署仍使用自身接口。
func TestBuiltinAIFallbackUsesProductionURL(t *testing.T) {
	for _, item := range []struct {
		environment string
		want        string
	}{
		{"", "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions"},
		{"https://goodhr.58it.cn", defaultBuiltinAIBaseURL},
		{"https://goodhr-test.ai.58it.cn", "https://goodhr-test.ai.58it.cn/api/ai-compatible/v1/chat/completions"},
		{"http://127.0.0.1:8084", "http://127.0.0.1:8084/api/ai-compatible/v1/chat/completions"},
	} {
		t.Run(item.environment, func(t *testing.T) {
			t.Setenv("GOODHR_PUBLIC_BASE_URL", item.environment)
			config, err := loadBuiltinAIConfig(nil)
			if err != nil {
				t.Fatal(err)
			}
			if config.PublicBaseURL != item.want {
				t.Fatalf("内置 AI 默认地址=%q，期望=%q", config.PublicBaseURL, item.want)
			}
		})
	}
}

// TestBuiltinAIStoredTransitURLRespectsEnvironment 验证旧直连配置回到所属环境，第三方、其他路径和相似域名保持原值。
func TestBuiltinAIStoredTransitURLRespectsEnvironment(t *testing.T) {
	for _, environment := range []string{"https://goodhr.58it.cn", "https://goodhr-test.ai.58it.cn", "http://127.0.0.1:8084"} {
		t.Setenv("GOODHR_PUBLIC_BASE_URL", environment)
		for _, item := range []struct {
			address string
			want    string
		}{
			{"https://goodhr.transit.ai.58it.cn/api/ai-compatible/v1/chat/completions", environment + "/api/ai-compatible/v1/chat/completions"},
			{"  HTTPS://GOODHR.TRANSIT.AI.58IT.CN/api/ai-compatible/v1/chat/completions  ", environment + "/api/ai-compatible/v1/chat/completions"},
			{"https://custom.example.com/v1/chat/completions", "https://custom.example.com/v1/chat/completions"},
			{"https://goodhr.transit.ai.58it.cn/other-route", "https://goodhr.transit.ai.58it.cn/other-route"},
			{"https://goodhr.transit.ai.58it.cn.external.test/api/ai-compatible/v1/chat/completions", "https://goodhr.transit.ai.58it.cn.external.test/api/ai-compatible/v1/chat/completions"},
		} {
			store := NewMemorySystemConfigStore()
			config := builtinAIPolicyConfigForTest()
			config.PublicBaseURL = item.address
			saveBuiltinAIConfigForTest(t, store, config)
			got, err := loadBuiltinAIConfig(store)
			if err != nil {
				t.Fatal(err)
			}
			if got.PublicBaseURL != item.want {
				t.Fatalf("环境=%q，已保存 AI 地址=%q，期望=%q", environment, got.PublicBaseURL, item.want)
			}
		}
	}
}

// TestBuiltinAITransitURLPostgresMigration 验证直连地址迁移覆盖全部旧站内配置，且只改地址、保留第三方并可重复执行。
func TestBuiltinAITransitURLPostgresMigration(t *testing.T) {
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
		COMMENT ON TABLE user_ai_configs IS 'AI 直连域名迁移测试配置';
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
	const migration = "0127_builtin_ai_transit_url.sql"
	const oldURL = "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions"
	const newURL = "https://goodhr.transit.ai.58it.cn/api/ai-compatible/v1/chat/completions"
	const externalURL = "https://custom.example.com/v1/chat/completions"
	seedTime := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	cases := []struct {
		baseURL string
		apiKey  string
		want    string
	}{
		{oldURL, "ghai_synthetic", newURL},
		{"  HTTPS://GOODHR.58IT.CN/api/ai-compatible/v1/chat/completions  ", "custom-synthetic", newURL},
		{"https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions", "custom-legacy", newURL},
		{"https://goodhr.ai.58it.cn/api/ai-compatible/v1/chat/completions", "", newURL},
		{externalURL, "ghai_external", externalURL},
		{externalURL, "custom-external", externalURL},
		{"https://goodhr.58it.cn.other.example/api/ai-compatible/v1/chat/completions", "ghai_similar", "https://goodhr.58it.cn.other.example/api/ai-compatible/v1/chat/completions"},
		{"https://goodhr.58it.cn/other-route", "ghai_other-route", "https://goodhr.58it.cn/other-route"},
		{newURL, "ghai_current", newURL},
		{"", "", ""},
	}
	snapshots := make(map[string]string)
	for index, item := range cases {
		email := fmt.Sprintf("transit-%d@example.test", index)
		var userID string
		if err := db.QueryRow(`INSERT INTO users(email) VALUES($1) RETURNING id::text`, email).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO user_ai_configs(user_id,base_url,model,api_key_encrypted,temperature,prompt_template,enabled,created_at,updated_at) VALUES($1::uuid,$2,'keep-model',$3,0.35,'保留测试提示词',$4,$5,$5)`, userID, item.baseURL, item.apiKey, index%2 == 0, seedTime); err != nil {
			t.Fatal(err)
		}
		var snapshot string
		if err := db.QueryRow(`SELECT (to_jsonb(ai)-'base_url')::text FROM user_ai_configs ai WHERE user_id=$1::uuid`, userID).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		snapshots[userID] = snapshot
	}
	aiRechargeApplyMigration(t, db, migration)
	aiRechargeApplyMigration(t, db, migration)
	for index, item := range cases {
		var userID, address, snapshot string
		if err := db.QueryRow(`SELECT ai.user_id::text,ai.base_url,(to_jsonb(ai)-'base_url')::text FROM user_ai_configs ai JOIN users u ON u.id=ai.user_id WHERE u.email=$1`, fmt.Sprintf("transit-%d@example.test", index)).Scan(&userID, &address, &snapshot); err != nil {
			t.Fatal(err)
		}
		if address != item.want || snapshot != snapshots[userID] {
			t.Fatalf("个人配置第 %d 项迁移范围或非地址字段发生变化", index)
		}
	}
	if _, err := db.Exec(`INSERT INTO system_configs(config_key,config_value,description,updated_at) VALUES('system.app_config','{}'::jsonb,'保留系统说明',$1)`, seedTime); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name   string
		before string
		want   string
	}{
		{"正式域名", `{"builtin_ai":{"public_base_url":"` + oldURL + `","default_model":"keep-model","upstream_api_key":"synthetic-upstream","models":[{"id":"keep-model"}]},"keep":{"x":1}}`, `{"builtin_ai":{"public_base_url":"` + newURL + `","default_model":"keep-model","upstream_api_key":"synthetic-upstream","models":[{"id":"keep-model"}]},"keep":{"x":1}}`},
		{"历史域名", `{"builtin_ai":{"public_base_url":"https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions"}}`, `{"builtin_ai":{"public_base_url":"` + newURL + `"}}`},
		{"另一个历史域名", `{"builtin_ai":{"public_base_url":"https://goodhr.ai.58it.cn/api/ai-compatible/v1/chat/completions"}}`, `{"builtin_ai":{"public_base_url":"` + newURL + `"}}`},
		{"空白与大小写", `{"builtin_ai":{"public_base_url":"  HTTPS://GOODHR.58IT.CN/api/ai-compatible/v1/chat/completions  "}}`, `{"builtin_ai":{"public_base_url":"` + newURL + `"}}`},
		{"空地址", `{"builtin_ai":{"public_base_url":"  ","default_model":"keep-model"}}`, `{"builtin_ai":{"public_base_url":"` + newURL + `","default_model":"keep-model"}}`},
		{"缺少地址", `{"builtin_ai":{"default_model":"keep-model"}}`, `{"builtin_ai":{"public_base_url":"` + newURL + `","default_model":"keep-model"}}`},
		{"缺少内置配置", `{"keep":1}`, `{"builtin_ai":{"public_base_url":"` + newURL + `"},"keep":1}`},
		{"内置配置为null", `{"builtin_ai":null,"keep":1}`, `{"builtin_ai":{"public_base_url":"` + newURL + `"},"keep":1}`},
		{"第三方地址", `{"builtin_ai":{"public_base_url":"` + externalURL + `","default_model":"keep-model"}}`, `{"builtin_ai":{"public_base_url":"` + externalURL + `","default_model":"keep-model"}}`},
		{"当前直连地址", `{"builtin_ai":{"public_base_url":"` + newURL + `"}}`, `{"builtin_ai":{"public_base_url":"` + newURL + `"}}`},
		{"无效根配置", `null`, `null`},
		{"无效内置配置", `{"builtin_ai":[],"keep":1}`, `{"builtin_ai":[],"keep":1}`},
	} {
		t.Run(item.name, func(t *testing.T) {
			if _, err := db.Exec(`UPDATE system_configs SET config_value=$1::jsonb WHERE config_key='system.app_config'`, item.before); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := db.QueryRow(`SELECT (to_jsonb(s)-'config_value')::text FROM system_configs s WHERE config_key='system.app_config'`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				aiRechargeApplyMigration(t, db, migration)
				var matches bool
				var after string
				if err := db.QueryRow(`SELECT config_value=$1::jsonb,(to_jsonb(s)-'config_value')::text FROM system_configs s WHERE config_key='system.app_config'`, item.want).Scan(&matches, &after); err != nil {
					t.Fatal(err)
				}
				if !matches || before != after {
					t.Fatalf("系统配置第 %d 次迁移未保留预期地址和其他字段", attempt+1)
				}
			}
		})
	}
}

// TestBuiltinAIURLPostgresMigration 验证迁移只修复带内置 Key 的历史地址，并保留第三方配置及其他系统字段。
func TestBuiltinAIURLPostgresMigration(t *testing.T) {
	db := aiRechargePostgresTestDB(t, true)
	if _, err := db.Exec(`
		CREATE TABLE user_ai_configs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			base_url TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			api_key_encrypted TEXT NOT NULL DEFAULT '',
			temperature NUMERIC(3, 2),
			prompt_template TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		COMMENT ON TABLE user_ai_configs IS '内置 AI 地址迁移测试配置';
		COMMENT ON COLUMN user_ai_configs.base_url IS '测试 AI 接口地址';
		COMMENT ON COLUMN user_ai_configs.api_key_encrypted IS '测试 AI 接口密钥';
	`); err != nil {
		t.Fatal(err)
	}

	const oldURL = "https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions"
	const newURL = "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions"
	const externalURL = "https://custom.example.com/v1/chat/completions"
	seedTime := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	for _, item := range []struct {
		email   string
		baseURL string
		apiKey  string
	}{
		{email: "builtin-old@example.test", baseURL: oldURL, apiKey: "ghai_builtin-old"},
		{email: "builtin-old-spaces@example.test", baseURL: "  HTTPS://GOODHR5.58IT.CN/api/ai-compatible/v1/chat/completions  ", apiKey: "ghai_builtin-spaces"},
		{email: "custom-on-old-host@example.test", baseURL: oldURL, apiKey: "custom-key"},
		{email: "builtin-external@example.test", baseURL: externalURL, apiKey: "ghai_user-selected-external"},
		{email: "custom-external@example.test", baseURL: externalURL, apiKey: "custom-external-key"},
		{email: "builtin-current@example.test", baseURL: newURL, apiKey: "ghai_builtin-current"},
	} {
		var userID string
		if err := db.QueryRow(`INSERT INTO users(email) VALUES($1) RETURNING id::text`, item.email).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO user_ai_configs(user_id,base_url,model,api_key_encrypted,updated_at) VALUES($1::uuid,$2,'test-model',$3,$4)`, userID, item.baseURL, item.apiKey, seedTime); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO system_configs(config_key,config_value,description,updated_at) VALUES('system.app_config',$1::jsonb,'测试系统配置',$2)`, `{"builtin_ai":{"public_base_url":"`+oldURL+`","default_model":"keep-model"},"keep":"keep-value"}`, seedTime); err != nil {
		t.Fatal(err)
	}

	aiRechargeApplyMigration(t, db, "0093_repair_builtin_ai_public_url.sql")
	for _, item := range []struct {
		email string
		want  string
	}{
		{email: "builtin-old@example.test", want: newURL},
		{email: "builtin-old-spaces@example.test", want: newURL},
		{email: "custom-on-old-host@example.test", want: oldURL},
		{email: "builtin-external@example.test", want: externalURL},
		{email: "custom-external@example.test", want: externalURL},
		{email: "builtin-current@example.test", want: newURL},
	} {
		var got string
		var updatedAt time.Time
		if err := db.QueryRow(`SELECT ai.base_url,ai.updated_at FROM user_ai_configs ai INNER JOIN users u ON u.id=ai.user_id WHERE u.email=$1`, item.email).Scan(&got, &updatedAt); err != nil {
			t.Fatal(err)
		}
		if got != item.want {
			t.Fatalf("%s 地址=%q，期望=%q", item.email, got, item.want)
		}
		if !updatedAt.Equal(seedTime) {
			t.Fatalf("%s 的更新时间不应被地址修复改写：%s", item.email, updatedAt)
		}
	}
	var systemURL, model, keep string
	var firstUpdated time.Time
	if err := db.QueryRow(`SELECT config_value #>> '{builtin_ai,public_base_url}',config_value #>> '{builtin_ai,default_model}',config_value->>'keep',updated_at FROM system_configs WHERE config_key='system.app_config'`).Scan(&systemURL, &model, &keep, &firstUpdated); err != nil {
		t.Fatal(err)
	}
	if systemURL != newURL || model != "keep-model" || keep != "keep-value" {
		t.Fatalf("系统配置迁移错误：url=%q model=%q keep=%q", systemURL, model, keep)
	}
	if !firstUpdated.Equal(seedTime) {
		t.Fatalf("系统配置更新时间不应被地址修复改写：%s", firstUpdated)
	}

	aiRechargeApplyMigration(t, db, "0093_repair_builtin_ai_public_url.sql")
	var secondUpdated time.Time
	if err := db.QueryRow(`SELECT updated_at FROM system_configs WHERE config_key='system.app_config'`).Scan(&secondUpdated); err != nil {
		t.Fatal(err)
	}
	if !secondUpdated.Equal(firstUpdated) {
		t.Fatalf("重复迁移不应再次改写系统配置时间：%s -> %s", firstUpdated, secondUpdated)
	}

	if _, err := db.Exec(`UPDATE system_configs SET config_value=jsonb_set(config_value,'{builtin_ai,public_base_url}','""'::jsonb,true),updated_at=$1 WHERE config_key='system.app_config'`, seedTime); err != nil {
		t.Fatal(err)
	}
	aiRechargeApplyMigration(t, db, "0093_repair_builtin_ai_public_url.sql")
	if err := db.QueryRow(`SELECT config_value #>> '{builtin_ai,public_base_url}' FROM system_configs WHERE config_key='system.app_config'`).Scan(&systemURL); err != nil || systemURL != newURL {
		t.Fatalf("空系统地址没有回到正式地址：url=%q err=%v", systemURL, err)
	}

	if _, err := db.Exec(`UPDATE system_configs SET config_value=jsonb_set(config_value,'{builtin_ai,public_base_url}',to_jsonb($1::text),true),updated_at=$2 WHERE config_key='system.app_config'`, externalURL, seedTime); err != nil {
		t.Fatal(err)
	}
	aiRechargeApplyMigration(t, db, "0093_repair_builtin_ai_public_url.sql")
	var customUpdated time.Time
	if err := db.QueryRow(`SELECT config_value #>> '{builtin_ai,public_base_url}',updated_at FROM system_configs WHERE config_key='system.app_config'`).Scan(&systemURL, &customUpdated); err != nil {
		t.Fatal(err)
	}
	if systemURL != externalURL || !customUpdated.Equal(seedTime) {
		t.Fatalf("自定义系统地址被迁移改写：url=%q updated=%s", systemURL, customUpdated)
	}
}
