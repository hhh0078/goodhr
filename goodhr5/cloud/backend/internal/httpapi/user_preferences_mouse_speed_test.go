// 文件作用说明：验证鼠标速度档位、旧版请求兼容和个人偏好隔离保存。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUserPreferencesMouseSpeedValidation 验证默认快速档、五个合法档位和不支持值的拒绝行为。
func TestUserPreferencesMouseSpeedValidation(t *testing.T) {
	recorder := httptest.NewRecorder()
	prefs, ok := (userPreferencesRequest{}).toPreferences(recorder)
	if !ok || prefs.MouseSpeed != "fast" || publicUserPreferences(UserPreferences{})["mouse_speed"] != "fast" {
		t.Fatalf("旧配置没有采用默认快速档：prefs=%+v ok=%v", prefs, ok)
	}
	for _, value := range []string{"ultra_fast", "fast", "standard", "relaxed", "slow"} {
		recorder := httptest.NewRecorder()
		prefs, ok := (userPreferencesRequest{MouseSpeed: &value}).toPreferences(recorder)
		if !ok || prefs.MouseSpeed != value {
			t.Fatalf("合法档位被拒绝：value=%q ok=%v body=%s", value, ok, recorder.Body.String())
		}
	}
	for _, value := range []string{"", "  ", "FAST", "unknown", "210", "2s"} {
		recorder := httptest.NewRecorder()
		if _, ok := (userPreferencesRequest{MouseSpeed: &value}).toPreferences(recorder); ok || recorder.Code != http.StatusBadRequest {
			t.Fatalf("不支持的档位通过校验：value=%q status=%d", value, recorder.Code)
		}
	}
}

// TestUserPreferencesMouseSpeedLegacySave 验证旧客户端保存其他设置时保留已选速度，且用户之间不会串配置。
func TestUserPreferencesMouseSpeedLegacySave(t *testing.T) {
	authStore := NewMemoryAuthStore()
	if err := authStore.SaveSession("speed-token", Session{Email: "speed@example.invalid", CreatedAt: time.Now()}, time.Hour); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryUserPreferencesStore()
	current := DefaultUserPreferences()
	current.MouseSpeed = "ultra_fast"
	current.CandidateReadConcurrency = 20
	current.DetailCloseDelayMin = 0.25
	current.DetailCloseDelayMax = 0.75
	if _, err := store.SaveUserPreferences("speed@example.invalid", current); err != nil {
		t.Fatal(err)
	}
	other := DefaultUserPreferences()
	other.MouseSpeed = "slow"
	if _, err := store.SaveUserPreferences("other@example.invalid", other); err != nil {
		t.Fatal(err)
	}
	service := NewUserPreferencesService(&AuthService{store: authStore}, store)
	for _, legacy := range []bool{true, false} {
		body := publicUserPreferences(current)
		body["ai_model"] = "updated-model"
		if legacy {
			delete(body, "mouse_speed")
		} else {
			body["mouse_speed"] = "relaxed"
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPut, "/api/config/user-preferences", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer speed-token")
		recorder := httptest.NewRecorder()
		service.User(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("保存失败：status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		saved, err := store.UserPreferences("speed@example.invalid")
		expected := "relaxed"
		if legacy {
			expected = "ultra_fast"
		}
		if err != nil || saved.MouseSpeed != expected || saved.AIModel != "updated-model" || saved.CandidateReadConcurrency != 20 || saved.DetailCloseDelayMin != 0.25 || saved.DetailCloseDelayMax != 0.75 {
			t.Fatalf("保存覆盖了其他偏好：legacy=%v prefs=%+v err=%v", legacy, saved, err)
		}
	}
	untouched, err := store.UserPreferences("other@example.invalid")
	if err != nil || untouched.MouseSpeed != "slow" {
		t.Fatalf("其他账号被修改：prefs=%+v err=%v", untouched, err)
	}
}

// TestUserPreferencesMouseSpeedPostgres 验证增量迁移保留旧偏好、允许重复执行和五档真实数据库读写。
func TestUserPreferencesMouseSpeedPostgres(t *testing.T) {
	db := articlePostgresTestDB(t)
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto; CREATE TABLE users(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),email TEXT UNIQUE NOT NULL); CREATE TABLE system_configs(config_key TEXT,config_value JSONB,enabled BOOLEAN);`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0011_user_preferences.sql", "0013_detail_prompt_and_preferences.sql", "0033_user_preference_delay_ranges.sql", "0081_user_preference_candidate_read_concurrency.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("preference migration %s: %v", name, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users(email) VALUES('legacy-speed@example.invalid'); INSERT INTO user_preferences(user_id,ai_model,candidate_read_concurrency,detail_close_delay_max) SELECT id,'保留旧模型',20,0.75 FROM users WHERE email='legacy-speed@example.invalid';`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.QueryRow(`SELECT to_jsonb(up)::text FROM user_preferences up`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0098_user_preference_mouse_speed.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("鼠标速度迁移 pass=%d err=%v", pass, err)
		}
	}
	var after, comment string
	if err = db.QueryRow(`SELECT (to_jsonb(up)-'mouse_speed')::text FROM user_preferences up`).Scan(&after); err != nil || before != after {
		t.Fatalf("迁移修改了旧字段：before=%s after=%s err=%v", before, after, err)
	}
	if err = db.QueryRow(`SELECT col_description('user_preferences'::regclass,attnum) FROM pg_attribute WHERE attrelid='user_preferences'::regclass AND attname='mouse_speed'`).Scan(&comment); err != nil || comment == "" {
		t.Fatalf("鼠标速度缺少中文备注：comment=%q err=%v", comment, err)
	}
	store := NewPostgresUserPreferencesStore(db)
	legacy, err := store.UserPreferences("legacy-speed@example.invalid")
	if err != nil || legacy.MouseSpeed != "fast" || legacy.AIModel != "保留旧模型" || legacy.CandidateReadConcurrency != 20 || legacy.DetailCloseDelayMax != 0.75 {
		t.Fatalf("旧配置升级异常：prefs=%+v err=%v", legacy, err)
	}
	for _, value := range []string{"ultra_fast", "fast", "standard", "relaxed", "slow"} {
		legacy.MouseSpeed = value
		saved, saveErr := store.SaveUserPreferences("legacy-speed@example.invalid", legacy)
		loaded, loadErr := store.UserPreferences("legacy-speed@example.invalid")
		if saveErr != nil || loadErr != nil || saved.MouseSpeed != value || loaded.MouseSpeed != value || loaded.AIModel != legacy.AIModel || loaded.CandidateReadConcurrency != 20 || loaded.DetailCloseDelayMax != 0.75 {
			t.Fatalf("档位保存失败：value=%s saved=%+v loaded=%+v err=%v/%v", value, saved, loaded, saveErr, loadErr)
		}
	}
	if _, err := db.Exec(`UPDATE user_preferences SET mouse_speed='unsupported'`); err == nil {
		t.Fatal("数据库没有拒绝未知鼠标速度档位")
	}
}
