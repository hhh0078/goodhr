// Package httpapi 本文件验证系统话术的权限、并发保护、岗位继承和旧版数据兼容。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestReplyLibraryInheritance 复现编辑、隐藏、恢复、管理员删除及老界面改其他字段等情形。
func TestReplyLibraryInheritance(t *testing.T) {
	lib, err := parseAutoReplyLibrary(initialAutoReplyLibrary)
	if err != nil || len(lib.Phrases) != 15 {
		t.Fatalf("默认话术异常: %v", err)
	}
	job := lib.Phrases[0]
	job.ID = "job_custom"
	job.Title = "自建"
	edit := lib.Phrases[1]
	edit.Content = "岗位自定义索简历"
	hidden := ReplyPhrase{ID: "receive_phone", Deleted: true}
	item := PositionAutoReplyConfig{PhraseOverrides: []ReplyPhrase{hidden, edit, job}}
	if err := prepareReplyOverrides(&item, nil, lib); err != nil {
		t.Fatal(err)
	}
	if item.ResumeRequestMessage != edit.Content || len(item.Phrases) != 15 || slices.Contains(item.Phrases, lib.Phrases[5]) {
		t.Fatal("岗位覆盖、删除或自建没有生效")
	}
	changed := lib
	changed.Phrases = slices.Clone(lib.Phrases)
	changed.Phrases[0].Content = "系统更新"
	changed.Phrases = slices.Delete(changed.Phrases, 1, 2)
	applyReplyLibrary(&item, changed)
	if item.Phrases[0].Content != "系统更新" || item.ResumeRequestMessage != edit.Content || item.PhrasesVersion == lib.revision() {
		t.Fatal("全局变化没有更新继承项或使自定义丢失")
	}
	legacy := PositionAutoReplyConfig{ResumeRequestMessage: "旧客户端新内容"}
	if err := prepareReplyOverrides(&legacy, item.PhraseOverrides, changed); err != nil || len(legacy.PhraseOverrides) != 3 || legacy.ResumeRequestMessage != "旧客户端新内容" {
		t.Fatalf("旧端保存覆盖其它条目: %v", err)
	}
	restore := PositionAutoReplyConfig{PhraseOverrides: []ReplyPhrase{}}
	if err := prepareReplyOverrides(&restore, item.PhraseOverrides, lib); err != nil || restore.ResumeRequestMessage != lib.Phrases[1].Content {
		t.Fatalf("恢复默认失败: %v", err)
	}
	empty, err := parseAutoReplyLibrary(`{"phrases":[],"condition_options":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	applyReplyLibrary(&restore, empty)
	if restore.Phrases == nil || len(restore.Phrases) != 0 || restore.ResumeRequestMessage != "" {
		t.Fatal("清空的库被默认值覆盖")
	}
}

// TestAdminReplyLibraryRejectsLostEdits 验证普通用户无权写入、非法内容被拒绝、过期管理员草稿不能覆盖新内容。
func TestAdminReplyLibraryRejectsLostEdits(t *testing.T) {
	t.Setenv("GOODHR_PG_DSN", "")
	t.Setenv("GOODHR_REDIS_ADDR", "")
	t.Setenv("GOODHR_SMTP_HOST", "")
	t.Setenv("GOODHR_SUPER_ADMINS", "phrases-admin@example.com")
	s := mustNewServer(t)
	routes := s.Routes()
	normal := loginForTest(t, routes, "phrase-user@example.com")
	admin := loginForTest(t, routes, "phrases-admin@example.com")
	record, err := s.systemConfigs.Get(autoReplyLibraryKey)
	if err != nil {
		t.Fatal(err)
	}
	put := func(token, raw, expect string) int {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"config_value": raw, "expected_config_value": expect})
		return rechargeTestRequest(routes, token, http.MethodPut, "/api/admin/system/configs/"+autoReplyLibraryKey, string(body)).Code
	}
	next := `{"phrases":[],"condition_options":[]}`
	if put(normal, next, record.ConfigValue) != 403 || put(admin, `{"phrases":null}`, record.ConfigValue) != 400 || put(admin, next, record.ConfigValue) != 200 || put(admin, record.ConfigValue, record.ConfigValue) != 409 {
		t.Fatal("管理员权限、输入校验或并发更新不符合预期")
	}
}

// TestPostgresReplyLibraryMigration 验证原岗位索简历内容不丢，反复执行迁移及还原覆盖不会重新冻结默认话术。
func TestPostgresReplyLibraryMigration(t *testing.T) {
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'; ALTER TABLE positions ADD COLUMN user_id UUID REFERENCES users(id)`); err != nil {
		t.Fatal(err)
	}
	applyAutoReplyTestMigrations(t, db)
	// 比较当前内存默认前先执行真实数据升级；公共旧版迁移夹具仍保留12项，供兼容测试复现升级起点。
	upgrade, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0121_auto_reply_wechat_defaults.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(upgrade)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var tenantID, posID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`WITH owner AS (INSERT INTO users(email,tenant_id) VALUES('phrase-owner@example.com',$1) RETURNING id) INSERT INTO positions(name,user_id) SELECT '测试',id FROM owner RETURNING id`, tenantID).Scan(&posID); err != nil {
		t.Fatal(err)
	}
	sys := NewPostgresSystemConfigStore(db)
	rec, err := sys.Get(autoReplyLibraryKey)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := parseAutoReplyLibrary(rec.ConfigValue)
	if err != nil {
		t.Fatal(err)
	}
	mem, _ := parseAutoReplyLibrary(initialAutoReplyLibrary)
	if mem.revision() != lib.revision() {
		t.Fatal("内存与数据库的默认资料不一致")
	}
	// 回到旧列形状，并使用超过新限制、符合老合同的真实长度验证迁移。
	oldText := strings.Repeat("文", 230)
	if _, err = db.Exec(`ALTER TABLE position_auto_reply_configs DROP COLUMN phrase_overrides`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO position_auto_reply_configs(position_id,tenant_id,resume_request_message)VALUES($1,$2,$3)`, posID, tenantID, oldText); err != nil {
		t.Fatal(err)
	}
	applyAutoReplyTestMigrations(t, db)
	store := NewPostgresAutoReplyStore(db)
	config, err := store.GetPositionAutoReplyConfig(ctx, tenantID, posID)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.PhraseOverrides) != 1 || config.PhraseOverrides[0].Content != oldText {
		t.Fatal("旧索简历话术迁移丢失")
	}
	if config, err = store.SavePositionAutoReplyConfig(ctx, "phrase-owner@example.com", config); err != nil {
		t.Fatal(err)
	}
	config.PhraseOverrides = []ReplyPhrase{}
	config, err = store.SavePositionAutoReplyConfig(ctx, "phrase-owner@example.com", config)
	if err != nil {
		t.Fatal(err)
	}
	applyAutoReplyTestMigrations(t, db)
	current, err := store.GetPositionAutoReplyConfig(ctx, tenantID, posID)
	if err != nil || len(current.PhraseOverrides) != 0 {
		t.Fatalf("重复迁移冻结了继承: %v", err)
	}
	updated := rec
	updated.ConfigValue = `{"phrases":[],"condition_options":[]}`
	if ok, err := sys.SaveIfUnchanged(updated, rec.ConfigValue); !ok || err != nil {
		t.Fatal("无法更新默认", err)
	}
	if ok, err := sys.SaveIfUnchanged(rec, rec.ConfigValue); ok || err != nil {
		t.Fatal("未拦截旧系统设置", err)
	}
	if _, err = store.SavePositionAutoReplyConfig(ctx, "phrase-owner@example.com", config); !errors.Is(err, errAutoReplyLibraryConflict) {
		t.Fatalf("管理员改默认后旧表单覆盖: %v", err)
	}
}
