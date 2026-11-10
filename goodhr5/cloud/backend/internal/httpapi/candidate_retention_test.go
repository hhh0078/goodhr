// 本文件验证临时简历的严格身份条件、维护鉴权、真实沟通续期和完整删除，仅使用独立PostgreSQL测试模式。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// onlineRetentionInt 为测试中的明确年龄和年份生成独立指针。
func onlineRetentionInt(value int) *int { return &value }

// TestOnlineProfileStrictIdentity 证明所有字段齐全、姓氏一致和年龄正负3岁同时满足，不能按缺失字段降级。
func TestOnlineProfileStrictIdentity(t *testing.T) {
	a := OnlineCandidateProfile{CandidateName: "邓云川", Age: onlineRetentionInt(28), EducationLevel: "大学本科", EarliestCompanyName: "某科技公司", EarliestCompanyYear: onlineRetentionInt(2020), EarliestSchoolName: "某大学", EarliestSchoolYear: onlineRetentionInt(2016)}
	b := a
	b.CandidateName = "邓某某"
	b.EducationLevel = "本科"
	b.Age = onlineRetentionInt(31)
	if !onlineProfilesMatch(a, b) {
		t.Fatal("用户确认的姓氏和完整字段规则未生效")
	}
	cases := map[string]func(*OnlineCandidateProfile){
		"年龄超界": func(p *OnlineCandidateProfile) { p.Age = onlineRetentionInt(32) },
		"年龄未知": func(p *OnlineCandidateProfile) { p.Age = nil },
		"年份未知": func(p *OnlineCandidateProfile) { p.EarliestCompanyYear = nil },
		"学历不同": func(p *OnlineCandidateProfile) { p.EducationLevel = "大专" },
		"学校不同": func(p *OnlineCandidateProfile) { p.EarliestSchoolName = "另一大学" },
		"姓氏不同": func(p *OnlineCandidateProfile) { p.CandidateName = "李云川" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := b
			change(&p)
			if onlineProfilesMatch(a, p) {
				t.Fatal("不完整或冲突资料被自动认作同人")
			}
		})
	}
	if onlineProfileSurname("欧阳云") != "欧阳" || onlineProfileSurname("John Smith") != "" {
		t.Fatal("姓氏识别不应按猜测认人")
	}
}

// retentionPostgresFixture 构造仅本测试可访问的团队与成员，不连接运行中的业务资料库。
func retentionPostgresFixture(t *testing.T) (*sql.DB, *PostgresCandidateStore, string, string) {
	t.Helper()
	db := autoReplyStateTestDB(t)
	applyCandidatePurgeSchema(t, db)
	var tenant, user string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(tenant_id,email) VALUES($1,'retention@example.test') RETURNING id::text`, tenant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	return db, NewPostgresCandidateStore(db), tenant, user
}

// retentionProfile 创建带明确资料更新时间的测试简历，测试不使用真实姓名、号码或附件。
func retentionProfile(t *testing.T, db *sql.DB, tenant, user, source, phone string, updated time.Time, contact *time.Time) string {
	t.Helper()
	var id string
	err := db.QueryRow(`INSERT INTO candidate_profiles(tenant_id,created_by_user_id,source_platform_id,source_platform_candidate_id,candidate_name,phone,created_at,updated_at,last_communication_at) VALUES($1,$2,'boss',$3,'合成候选人',$4,$5,$5,$6) RETURNING id::text`, tenant, user, source, phone, updated, contact).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO candidate_profile_sources(tenant_id,candidate_id,platform_id,platform_candidate_id,source_type,online_profile,created_at,updated_at) VALUES($1,$2,'boss',$3,'automatic','{"profile_key":"synthetic-online","profile":{"raw_text":"合成在线临时资料"}}',$4,$4)`, tenant, id, source, updated); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestTemporaryRetentionRechecksInsideDelete 验证相同简历版本下新沟通或补手机号也可阻止已经选中的过期清理。
func TestTemporaryRetentionRechecksInsideDelete(t *testing.T) {
	db, store, tenant, user := retentionPostgresFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	cutoff := now.Add(-30 * 24 * time.Hour)
	for _, kind := range []string{"communication", "phone"} {
		t.Run(kind, func(t *testing.T) {
			id := retentionProfile(t, db, tenant, user, kind, "", cutoff.Add(-time.Hour), nil)
			if kind == "communication" {
				if _, err := db.Exec(`UPDATE candidate_profiles SET last_communication_at=$2 WHERE id=$1`, id, now); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.Exec(`UPDATE candidate_profiles SET phone='13800000000' WHERE id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			err := store.purgeCandidate(ctx, CandidateLibraryScope{TenantID: tenant, IsAdmin: true, retentionCutoff: &cutoff}, id, 1)
			if !errors.Is(err, errCandidateRetentionChanged) {
				t.Fatalf("没有在删除事务内保护新资料：%v", err)
			}
			var count int
			if err = db.QueryRow(`SELECT count(*) FROM candidate_profiles WHERE id=$1`, id).Scan(&count); err != nil || count != 1 {
				t.Fatal("误删", err, count)
			}
		})
	}
}

// TestTemporaryRetentionJobKeepsFreshAndPhone 验证30天严格边界、真实近期沟通、有效手机、掩码手机与重复请求防重。
func TestTemporaryRetentionJobKeepsFreshAndPhone(t *testing.T) {
	db, store, tenant, user := retentionPostgresFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	cutoff := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	old := retentionProfile(t, db, tenant, user, "old", "", cutoff.Add(-time.Hour), nil)
	masked := retentionProfile(t, db, tenant, user, "masked", "138****0000", cutoff.Add(-time.Hour), nil)
	boundary := retentionProfile(t, db, tenant, user, "boundary", "", cutoff, nil)
	active := retentionProfile(t, db, tenant, user, "active", "", cutoff.Add(-time.Hour), &recent)
	formal := retentionProfile(t, db, tenant, user, "formal", "13800000000", cutoff.Add(-time.Hour), nil)
	var jobID string
	if err := db.QueryRow(`INSERT INTO candidate_retention_jobs(request_day,cutoff,status) VALUES('2026-11-10',$1,'running') RETURNING id::text`, cutoff).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	service := &CandidateRetentionService{store: store, now: func() time.Time { return now }}
	if err := service.runJob(ctx, CandidateRetentionJob{ID: jobID, Cutoff: cutoff}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id   string
		want int
	}{{old, 0}, {masked, 0}, {boundary, 1}, {active, 1}, {formal, 1}} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM candidate_profiles WHERE id=$1`, item.id).Scan(&count); err != nil || count != item.want {
			t.Fatal("保留边界不符", count, item.want, err)
		}
	}
	job, err := scanCandidateRetentionJob(db.QueryRow(`SELECT id::text,cutoff,status,deleted_count,skipped_count,updated_at,error FROM candidate_retention_jobs WHERE id=$1`, jobID))
	if err != nil || job.Status != "completed" || job.DeletedCount != 2 {
		t.Fatal("清理进度不符", job, err)
	}
	if err = service.runJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM candidate_deletions WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 2 {
		t.Fatal("重复执行创建了重复删除", count, err)
	}
}

// TestTemporaryRetentionRejectsUntrustedTrigger 维护入口不接受登录态、URL密钥或无密钥请求。
func TestTemporaryRetentionRejectsUntrustedTrigger(t *testing.T) {
	token := strings.Repeat("test-only-", 5)
	t.Setenv("GOODHR_TEMP_RESUME_CLEANUP_TOKEN", token)
	service := &CandidateRetentionService{}
	for _, tc := range []struct {
		method, url, auth string
		status            int
	}{{"GET", "/cleanup?token=" + token, "", 401}, {"POST", "/cleanup", "Bearer wrong", 401}, {"DELETE", "/cleanup", "Bearer " + token, 405}, {"POST", "/cleanup", "Bearer " + token, 503}} {
		req := httptest.NewRequest(tc.method, tc.url, nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		w := httptest.NewRecorder()
		service.Cleanup(w, req)
		if w.Code != tc.status {
			t.Fatal("维护鉴权状态不符", w.Code, tc.status)
		}
	}
}

// TestTemporaryRetentionHTTPResumesBatches 验证每天只请求一次即可分批完成，重复请求与崩溃后过期租约不会产生第二份任务。
func TestTemporaryRetentionHTTPResumesBatches(t *testing.T) {
	db, store, tenant, user := retentionPostgresFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.Add(-31 * 24 * time.Hour)
	if _, err := db.Exec(`INSERT INTO candidate_profiles(tenant_id,created_by_user_id,source_platform_id,source_platform_candidate_id,candidate_name,created_at,updated_at) SELECT $1,$2,'boss','synthetic-'||n,'合成临时人选',$3,$3 FROM generate_series(1,101) AS n`, tenant, user, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO candidate_profile_sources(tenant_id,candidate_id,platform_id,platform_candidate_id,source_type,online_profile,created_at,updated_at) SELECT tenant_id,id,source_platform_id,source_platform_candidate_id,'automatic','{"profile_key":"synthetic-online","profile":{"raw_text":"合成在线临时资料"}}',created_at,updated_at FROM candidate_profiles WHERE tenant_id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("test-only-token-", 3)
	t.Setenv("GOODHR_TEMP_RESUME_CLEANUP_TOKEN", token)
	service := &CandidateRetentionService{store: store, now: func() time.Time { return now }}
	service.running.Store(true) // 模拟请求已入库但执行器尚未启动，便于核验重复触发和重启恢复。
	var first CandidateRetentionJob
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/internal/temporary-resumes/cleanup", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		service.Cleanup(w, req)
		var job CandidateRetentionJob
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &job) != nil || job.ID == "" {
			t.Fatal("未登记维护任务", w.Code, w.Body.String())
		}
		if i == 0 {
			first = job
		} else if job.ID != first.ID {
			t.Fatal("同日重复创建了任务")
		}
	}
	if _, err := db.Exec(`UPDATE candidate_retention_jobs SET status='running',lease_until=now()-interval '1 second' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	service.running.Store(false)
	service.launch()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest("GET", "/api/internal/temporary-resumes/cleanup", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		service.Cleanup(w, req)
		var job CandidateRetentionJob
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &job) != nil {
			t.Fatal("读不到维护进度", w.Code)
		}
		if job.Status == "failed" {
			t.Fatal("分批清理失败", job.Error)
		}
		if job.Status == "completed" && !service.running.Load() {
			if job.DeletedCount != 101 {
				t.Fatal("一次请求未完成多批资料", job.DeletedCount)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM candidate_retention_jobs`).Scan(&count); err != nil || count != 1 {
				t.Fatal("维护任务不幂等", count, err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond) // 只等待后台任务的实际进度，不给页面操作添加固定延时。
	}
	t.Fatal("后台维护任务未在测试期限内完成")
}

// retentionStateChange 构造不含真实个人资料的服务器状态记录。
func retentionStateChange(t *testing.T, kind, id string, payload any, at time.Time) AutoReplyStateChange {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return AutoReplyStateChange{Kind: kind, ID: id, Revision: 1, Sequence: 1, UpdatedAt: at.Format(time.RFC3339Nano), Payload: raw}
}

// TestCandidateCommunicationUsesPlatformTime 验证已处理消息重放不按当前时间续期，系统提示也不延长保留期。
func TestCandidateCommunicationUsesPlatformTime(t *testing.T) {
	db, _, tenant, user := retentionPostgresFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	past := now.Add(-time.Hour)
	old := now.Add(-40 * 24 * time.Hour)
	id := retentionProfile(t, db, tenant, user, "source", "", old, nil)
	store := NewPostgresAutoReplyStore(db)
	changes := []AutoReplyStateChange{
		retentionStateChange(t, "candidates", "local", map[string]any{"ID": "local"}, old),
		retentionStateChange(t, "identities", "identity", map[string]any{"candidate_id": "local", "platform_id": "boss", "platform_candidate_id": "source"}, old),
		retentionStateChange(t, "conversations", "conversation", map[string]any{"candidate_id": "local", "platform_identity_id": "identity", "platform_id": "boss"}, old),
		retentionStateChange(t, "messages", "message", map[string]any{"conversation_id": "conversation", "direction": "candidate", "platform_sent_at": past}, now),
	}
	version, err := store.CommitAutoReplyState(ctx, tenant, user, 0, changes)
	if err != nil {
		t.Fatal(err)
	}
	var contact, updated time.Time
	if err = db.QueryRow(`SELECT last_communication_at,updated_at FROM candidate_profiles WHERE id=$1`, id).Scan(&contact, &updated); err != nil || !contact.Equal(past) || !updated.Equal(old) {
		t.Fatal("真实沟通时间不符", contact, updated, err)
	}
	changes = changes[3:]
	changes[0].Revision = 2
	changes[0].UpdatedAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	if _, err = store.CommitAutoReplyState(ctx, tenant, user, version, changes); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT last_communication_at FROM candidate_profiles WHERE id=$1`, id).Scan(&contact); err != nil || !contact.Equal(past) {
		t.Fatal("重放错误续期", contact, err)
	}
}

// TestTemporaryRetentionCleansCacheWithoutLibrary 验证未开启正式入库的临时在线资料也会完整清理，并保护真实新沟通和已取得手机号。
func TestTemporaryRetentionCleansCacheWithoutLibrary(t *testing.T) {
	db, store, tenant, user := retentionPostgresFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.Add(-31 * 24 * time.Hour)
	cutoff := now.Add(-30 * 24 * time.Hour)
	state := NewPostgresAutoReplyStore(db)
	version := int64(0)
	for _, item := range []struct {
		source, phone string
		at            time.Time
	}{{"expired-cache", "", old}, {"active-cache", "", now.Add(-time.Hour)}, {"phone-cache", "13800000000", old}} {
		changes := []AutoReplyStateChange{
			retentionStateChange(t, "candidates", item.source, map[string]any{"ID": item.source, "Phone": item.phone, "RawText": "合成在线资料", "online_profile": map[string]any{"candidate_id": "", "updated_at": old}}, old),
			retentionStateChange(t, "identities", "identity-"+item.source, map[string]any{"candidate_id": item.source, "platform_id": "boss", "platform_candidate_id": item.source}, old),
			retentionStateChange(t, "conversations", "conversation-"+item.source, map[string]any{"candidate_id": item.source, "platform_identity_id": "identity-" + item.source, "platform_id": "boss", "platform_account_id": "account", "platform_thread_id": item.source}, old),
			retentionStateChange(t, "messages", "message-"+item.source, map[string]any{"conversation_id": "conversation-" + item.source, "direction": "candidate", "platform_sent_at": item.at}, item.at),
		}
		var err error
		version, err = state.CommitAutoReplyState(ctx, tenant, user, version, changes)
		if err != nil {
			t.Fatal(err)
		}
	}
	var jobID string
	if err := db.QueryRow(`INSERT INTO candidate_retention_jobs(request_day,cutoff,status) VALUES('2026-11-10',$1,'running') RETURNING id::text`, cutoff).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	service := &CandidateRetentionService{store: store, now: func() time.Time { return now }}
	if err := service.runJob(ctx, CandidateRetentionJob{ID: jobID, Cutoff: cutoff}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id   string
		want int
	}{{"expired-cache", 0}, {"active-cache", 1}, {"phone-cache", 1}} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='candidates' AND id=$3`, tenant, user, item.id).Scan(&count); err != nil || count != item.want {
			t.Fatal("未入库资料过期处理不符", count, item.want, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM candidate_profiles WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("清理中间主档不应进入简历库", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND kind IN ('messages','conversations','identities') AND id LIKE '%expired-cache'`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("临时聊天和身份残留", count, err)
	}
}
