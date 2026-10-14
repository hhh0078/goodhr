// 本文件验证岗位独立开关、历史客户端兼容、推荐筛选和关闭岗位写入保护。
package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestPositionOpenHTTP 验证开关独立于运行状态，关闭仅隐藏岗位，历史推荐仍保留。
func TestPositionOpenHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "open-position@example.com")
	other := loginForTest(t, routes, "other-position@example.com")
	status, created := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{"name": "开启状态测试"})
	if status != 200 || created["position"].(map[string]any)["is_open"] != true {
		t.Fatalf("new default=%v", created)
	}
	positionID := created["position"].(map[string]any)["id"].(string)
	_, candidate := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "岗位开关人选", "phone": "13800138950"})
	candidateID := candidate["candidate"].(map[string]any)["id"].(string)
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/"+candidateID+"/recommendations", map[string]any{"position_id": positionID})
	if status != 200 {
		t.Fatal("open position recommendation failed")
	}
	status, closed := candidateTestRequest(t, routes, token, http.MethodPatch, "/api/positions/"+positionID, map[string]any{"is_open": false})
	if status != 200 || closed["position"].(map[string]any)["is_open"] != false || closed["position"].(map[string]any)["status"] != "created" {
		t.Fatal("close changed runtime or failed", closed)
	}
	status, start := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions/"+positionID+"/start", map[string]any{"task_type": "keyword"})
	if status != 409 || start["error"].(map[string]any)["code"] != "POSITION_CLOSED" {
		t.Fatalf("closed start=%d %v", status, start)
	}
	for _, check := range []struct {
		query string
		count int
	}{{"", 0}, {"?is_open=false", 1}, {"?is_open=all", 1}, {"?purpose=recommendation&is_open=all", 0}, {"?purpose=recommendation&is_open=false", 0}} {
		status, listed := candidateTestRequest(t, routes, token, http.MethodGet, "/api/positions"+check.query, nil)
		if status != 200 || len(listed["positions"].([]any)) != check.count {
			t.Fatalf("query=%s listed=%v", check.query, listed)
		}
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/"+candidateID+"/recommendations", map[string]any{"position_id": positionID, "stage": "recommended"})
	if status != 409 {
		t.Fatalf("closed recommendation status=%d", status)
	}
	status, history := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates/"+candidateID+"/recommendations", nil)
	if status != 200 || len(history["recommendations"].([]any)) != 1 {
		t.Fatal("close removed recommendation history", history)
	}
	status, _ = candidateTestRequest(t, routes, other, http.MethodPatch, "/api/positions/"+positionID, map[string]any{"is_open": true})
	if status != 404 {
		t.Fatal("other account toggled position")
	}
	status, saved := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{"id": positionID, "name": "旧客户端保存"})
	if status != 200 || saved["position"].(map[string]any)["is_open"] != false {
		t.Fatal("old client reopened closed position", saved)
	}
	status, reopened := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{"id": positionID, "name": "编辑开启", "is_open": true})
	if status != 200 || reopened["position"].(map[string]any)["is_open"] != true {
		t.Fatal("edit switch not saved", reopened)
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{"name": "新建关闭", "is_open": false})
	if status != 200 {
		t.Fatal("closed create rejected")
	}
	_, exchange := candidateTestRequest(t, routes, token, http.MethodPost, "/api/auth/extension-session", map[string]any{})
	status, pluginList := candidateTestRequest(t, routes, exchange["token"].(string), http.MethodGet, "/api/positions?is_open=all", nil)
	if status != 200 || len(pluginList["positions"].([]any)) != 1 {
		t.Fatal("plugin saw closed position", pluginList)
	}
}

// TestPositionOpenPostgres 验证岗位列迁移、旧客户端保存、开启开关和事务内推荐保护。
func TestPositionOpenPostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	// 在独立schema补齐岗位查询所需的历史字段，测试复用真实岗位存储代码。
	setup := `ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
 ALTER TABLE positions DROP COLUMN is_open;
 ALTER TABLE positions ADD COLUMN user_id UUID REFERENCES users(id), ADD COLUMN platform_id TEXT NOT NULL DEFAULT 'boss',
 ADD COLUMN keywords JSONB NOT NULL DEFAULT '[]',ADD COLUMN exclude_keywords JSONB NOT NULL DEFAULT '[]',
 ADD COLUMN description TEXT NOT NULL DEFAULT '',ADD COLUMN greet_message TEXT NOT NULL DEFAULT '',ADD COLUMN is_and_mode BOOLEAN NOT NULL DEFAULT false,
 ADD COLUMN common_config JSONB NOT NULL DEFAULT '{}',ADD COLUMN ai_config JSONB NOT NULL DEFAULT '{}',ADD COLUMN keyword_config JSONB NOT NULL DEFAULT '{}',
 ADD COLUMN match_limit INTEGER NOT NULL DEFAULT 50,ADD COLUMN status TEXT NOT NULL DEFAULT 'created',ADD COLUMN scanned_count INTEGER NOT NULL DEFAULT 0,
 ADD COLUMN daily_greeted_count INTEGER NOT NULL DEFAULT 0,ADD COLUMN daily_greeted_date DATE NOT NULL DEFAULT CURRENT_DATE,
 ADD COLUMN skipped_count INTEGER NOT NULL DEFAULT 0,ADD COLUMN failed_count INTEGER NOT NULL DEFAULT 0,
 ADD COLUMN enable_sound BOOLEAN NOT NULL DEFAULT false,ADD COLUMN enable_thinking BOOLEAN NOT NULL DEFAULT false,
 ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now(),ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),ADD COLUMN started_at TIMESTAMPTZ,ADD COLUMN finished_at TIMESTAMPTZ;`
	if _, err := db.Exec(setup); err != nil {
		t.Fatal(err)
	}
	var tenantID, userID, legacyID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	email := "position-pg-open@example.com"
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id) VALUES($1,$2) RETURNING id::text`, email, tenantID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(user_id,name,status) VALUES($1,'历史岗位','completed') RETURNING id::text`, userID).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0095_position_open.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	positions := NewPostgresPositionStore(db)
	legacy, err := positions.PositionByID(tenantID, email, legacyID, false)
	if err != nil || !legacy.IsOpen || legacy.Status != "completed" {
		t.Fatal("legacy position default changed", legacy, err)
	}
	position, err := positions.SavePosition(Position{UserEmail: email, Name: "PG开关"})
	if err != nil || !position.IsOpen {
		t.Fatal("default new position closed", err)
	}
	candidates := NewPostgresCandidateStore(db)
	scope := CandidateLibraryScope{TenantID: tenantID, UserEmail: email, IsAdmin: true}
	fields, _ := candidateMutationFields(map[string]any{"candidate_name": "PG开关人选", "phone": "13800138951"}, true)
	candidate, err := candidates.SaveLibraryCandidate(scope, "", fields, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = candidates.SaveCandidateRecommendation(scope, candidate.ID, position, "draft", "", "first"); err != nil {
		t.Fatal(err)
	}
	if err = positions.ClaimPositionStart(email, position.ID); err != nil {
		t.Fatal(err)
	}
	closed, err := positions.SetPositionOpen(email, position.ID, false)
	if err != nil || closed.IsOpen || closed.Status != "running" {
		t.Fatal("closing changed running status", closed, err)
	}
	if err = positions.ClaimPositionStart(email, position.ID); !errors.Is(err, ErrCandidatePositionClosed) {
		t.Fatal("closed job claimed", err)
	}
	// 传入缓存中的开启岗位快照，真实数据库仍必须拦截关闭岗位的新增推荐。
	if _, err = candidates.SaveCandidateRecommendation(scope, candidate.ID, position, "recommended", "", "stale"); !errors.Is(err, ErrCandidatePositionClosed) {
		t.Fatal("stale open snapshot bypassed database check", err)
	}
	history, err := candidates.ListCandidateRecommendations(scope, candidate.ID)
	if err != nil || len(history) != 1 || history[0].Stage != "draft" {
		t.Fatal("closing removed or advanced history", history, err)
	}
	saved, err := positions.SavePosition(Position{ID: position.ID, UserEmail: email, Name: "旧请求保存"})
	if err != nil || saved.IsOpen || saved.Status != "running" {
		t.Fatal("legacy save changed open status", saved, err)
	}
	listed, err := positions.ListPositions(tenantID, email, false)
	if err != nil || len(filterOpenPositions(listed, "")) != 1 || len(filterOpenPositions(listed, "false")) != 1 {
		t.Fatal("PG open list filter", listed, err)
	}
}
