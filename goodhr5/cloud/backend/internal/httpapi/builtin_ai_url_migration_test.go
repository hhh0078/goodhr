// 本文件验证内置 AI 默认正式地址及历史地址迁移，不改动用户自定义的第三方接口。
package httpapi

import (
	"testing"
	"time"
)

// TestBuiltinAIFallbackUsesProductionURL 验证未配置部署域名时仍返回正式内置 AI 地址。
func TestBuiltinAIFallbackUsesProductionURL(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "")
	config, err := loadBuiltinAIConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.PublicBaseURL != "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions" {
		t.Fatalf("内置 AI 默认地址错误：%s", config.PublicBaseURL)
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
