// 本文件在独立PostgreSQL模式验证在线建档、严格唯一关联、指纹幂等与删后当前代续写证明。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// onlineProfileFixture 构造真实成员、岗位、会话来信和已完成的同设备资料读取收据。
func onlineProfileFixture(t *testing.T) (editableFactFixture, *PostgresCandidateStore, onlineProfileStoreInput) {
	t.Helper()
	f := newEditableFactFixture(t)
	store := NewPostgresCandidateStore(f.db)
	age, work, school := 28, 2015, 2012
	p := OnlineCandidateProfile{CandidateName: "邓云川", Age: &age, EducationLevel: "大专", EarliestCompanyName: "甲公司", EarliestCompanyYear: &work, EarliestSchoolName: "乙学校", EarliestSchoolYear: &school, RawText: "邓云川 28岁 大专；2015年甲公司；2012年乙学校", SourceKey: "online-image:synthetic-source", WorkExperiences: []CandidateWorkExperience{{CompanyName: "甲公司", StartYM: "2015-01"}}, Educations: []CandidateEducation{{SchoolName: "乙学校", StartYM: "2012-09"}}}
	in := onlineProfileStoreInput{Scope: CandidateLibraryScope{TenantID: f.tenant, UserEmail: "fact-hr@example.test", IsAdmin: true}, UserID: f.user, MachineID: "synthetic-machine", Structured: true, Request: autoReplyOnlineProfileRequest{PositionID: f.pos, ConversationID: "online-thread", PlatformID: "boss", PlatformAccountID: "郭星", PlatformCandidateID: "754299372", Profile: p}}
	f.entity(t, "candidates", "online-local", map[string]any{"ID": "online-local", "CandidateName": p.CandidateName})
	f.entity(t, "identities", "online-identity", map[string]any{"candidate_id": "online-local", "platform_id": "boss", "platform_account_id": "郭星", "platform_candidate_id": "754299372"})
	f.entity(t, "conversations", "online-thread", map[string]any{"candidate_id": "online-local", "platform_identity_id": "online-identity", "position_id": f.pos, "platform_id": "boss", "platform_account_id": "郭星", "platform_thread_id": "754299372-0", "candidate_name": p.CandidateName})
	arrived := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	f.entity(t, "messages", "online-inbound", map[string]any{"conversation_id": "online-thread", "direction": "candidate", "fingerprint": "new-question", "platform_sent_at": arrived})
	f.entity(t, "runtime_commands", "online-read", map[string]any{"action": "read_online_profile", "status": "completed", "position_id": f.pos, "machine_id": in.MachineID, "platform_account_id": "郭星", "platform_thread_id": "754299372-0", "conversation_id": "online-thread", "source_key": p.SourceKey, "updated_at": arrived.Add(time.Minute)})
	return f, store, in
}

// cacheOnlineFixture 模拟服务器流程接收API结果后在原候选人事务里保存缓存，不修改任何业务身份编号。
func cacheOnlineFixture(t *testing.T, f editableFactFixture, in onlineProfileStoreInput, result autoReplyOnlineProfileResult) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"candidate_id": result.CandidateID, "profile_key": result.ProfileKey, "source_key": in.Request.Profile.SourceKey, "profile": in.Request.Profile})
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{online_profile}',$1::jsonb) WHERE tenant_id=$2 AND user_id=$3 AND kind='candidates' AND id='online-local'`, string(raw), f.tenant, f.user); err != nil {
		t.Fatal(err)
	}
}

// TestOnlineProfilePGIdempotentAndPhoneContinuation 验证不含手机号的临时档可补真实号码，重复结果不更新时间或覆盖正式姓名。
func TestOnlineProfilePGIdempotentAndPhoneContinuation(t *testing.T) {
	f, store, in := onlineProfileFixture(t)
	ctx := context.Background()
	first, err := store.saveOnlineProfile(ctx, in)
	if err != nil || first.CandidateID == "" || first.MatchStatus != "temporary" {
		t.Fatalf("在线临时档未创建：%+v %v", first, err)
	}
	var before, after time.Time
	var version, again int64
	var sourceBefore, sourceAfter time.Time
	if err = f.db.QueryRow(`SELECT cp.updated_at,cp.row_version,cs.updated_at FROM candidate_profiles cp JOIN candidate_profile_sources cs ON cs.candidate_id=cp.id WHERE cp.id=$1`, first.CandidateID).Scan(&before, &version, &sourceBefore); err != nil {
		t.Fatal(err)
	}
	second, err := store.saveOnlineProfile(ctx, in)
	if err != nil || second.CandidateID != first.CandidateID {
		t.Fatal("同指纹创建了第二份", err)
	}
	if err = f.db.QueryRow(`SELECT cp.updated_at,cp.row_version,cs.updated_at FROM candidate_profiles cp JOIN candidate_profile_sources cs ON cs.candidate_id=cp.id WHERE cp.id=$1`, first.CandidateID).Scan(&after, &again, &sourceAfter); err != nil {
		t.Fatal(err)
	}
	if !before.Equal(after) || version != again || !sourceBefore.Equal(sourceAfter) {
		t.Fatal("相同资料刷新了版本或资料时间")
	}
	otherDevice := in
	otherDevice.MachineID = "new-bound-device"
	if _, err = store.saveOnlineProfile(ctx, otherDevice); err == nil {
		t.Fatal("无服务器缓存时新设备复用了旧设备采集命令")
	}
	cacheOnlineFixture(t, f, in, first)
	if cached, cacheErr := store.saveOnlineProfile(ctx, otherDevice); cacheErr != nil || cached.CandidateID != first.CandidateID {
		t.Fatalf("当前代同会话缓存不能跨设备复用：%+v %v", cached, cacheErr)
	}
	forged := otherDevice
	forged.Request.Profile.RawText += "伪造的资料变化"
	if _, err := store.saveOnlineProfile(ctx, forged); err == nil {
		t.Fatal("新设备借旧缓存接受了不同指纹资料")
	}
	input := CandidateProfileInput{TenantID: f.tenant, UserEmail: in.Scope.UserEmail, PlatformID: "boss", PlatformCandidateID: in.Request.PlatformCandidateID, AutoReplyPositionID: f.pos, ReopenPlatformAccount: "郭星", ReopenServerWitness: true, ReopenIsAdmin: true, OnlineProfileCandidateID: first.CandidateID, OnlineProfileKey: first.ProfileKey, OnlineProfileMachineID: in.MachineID, CandidateName: "不应覆盖", Phone: "13900001111", Ext: map[string]any{"structured_fields": map[string]any{}}}
	saved, err := store.SaveCandidateProfile(input)
	if err != nil || saved.ID != first.CandidateID || saved.Phone != input.Phone || saved.CandidateName != "邓云川" {
		t.Fatalf("补手机号失败或覆盖姓名：%+v %v", saved, err)
	}
	input.OnlineProfileMachineID = otherDevice.MachineID
	if current, deviceErr := store.SaveCandidateProfile(input); deviceErr != nil || current.ID != first.CandidateID {
		t.Fatalf("已有当前代精确缓存的新设备不能继续补齐：%+v %v", current, deviceErr)
	}
	for _, bad := range []CandidateProfileInput{func() CandidateProfileInput { v := input; v.OnlineProfileKey = purgeDigest("wrong"); return v }(), func() CandidateProfileInput { v := input; v.ReopenServerWitness = false; return v }(), func() CandidateProfileInput { v := input; v.ReopenPlatformAccount = "另一个账号"; return v }(), func() CandidateProfileInput { v := input; v.UserEmail = "peer-hr@example.test"; return v }()} {
		if _, err := store.SaveCandidateProfile(bad); err == nil {
			t.Fatal("错误在线续写证明被接受")
		}
	}
	otherDevice.Request.PlatformAccountID = "另一个账号"
	if _, err := store.saveOnlineProfile(ctx, otherDevice); err == nil {
		t.Fatal("其它账号复用了当前账号缓存")
	}
}

// TestOnlineProfilePGStructuredSwitchIsRespected 验证岗位关闭正式入库时只返回服务器缓存指纹，不创建简历库记录。
func TestOnlineProfilePGStructuredSwitchIsRespected(t *testing.T) {
	f, store, in := onlineProfileFixture(t)
	in.Structured = false
	result, err := store.saveOnlineProfile(context.Background(), in)
	if err != nil || result.CandidateID != "" || result.ProfileKey == "" || result.MatchStatus != "temporary" {
		t.Fatalf("缓存结果不正确：%+v %v", result, err)
	}
	var count int
	if err = f.db.QueryRow("SELECT count(*) FROM candidate_profiles WHERE tenant_id=$1", f.tenant).Scan(&count); err != nil || count != 0 {
		t.Fatalf("关闭结构化开关仍然入库：%d %v", count, err)
	}
}

// TestOnlineProfilePGDeletedSourceNeedsNewRead 验证新在线资料可在无手机无附件时重建，旧回包与旧代次仍被封堵。
func TestOnlineProfilePGDeletedSourceNeedsNewRead(t *testing.T) {
	f, store, in := onlineProfileFixture(t)
	source := purgeSourceHash("boss", in.Request.PlatformCandidateID)
	cutoff := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	var seq int64
	if err := f.db.QueryRow(`INSERT INTO candidate_deletions(tenant_id,candidate_id,row_version,owner_hash,audiences,source_hashes,deleted_at) VALUES($1,gen_random_uuid(),1,'old',jsonb_build_object($2::text,'[]'::jsonb),jsonb_build_array($3::text),$4) RETURNING sequence`, f.tenant, purgeDigest(in.Scope.UserEmail), source, cutoff).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO candidate_deletion_keys(tenant_id,kind,identity_hash) VALUES($1,'source',$2)`, f.tenant, source); err != nil {
		t.Fatal(err)
	}
	var arrived time.Time
	if err := f.db.QueryRow(`SELECT (payload->>'platform_sent_at')::timestamptz FROM auto_reply_cloud_entities WHERE kind='messages' AND id='online-inbound'`).Scan(&arrived); err != nil {
		t.Fatal(err)
	}
	change, _ := json.Marshal(map[string]any{"deletion_boundary": map[string]any{"sequence": seq, "source_hash": source, "deleted_at": cutoff}, "reopen_inbound_hash": purgeDigest("new-question"), "reopen_inbound_at": arrived})
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=payload||$1::jsonb WHERE kind='conversations' AND id='online-thread'`, string(change)); err != nil {
		t.Fatal(err)
	}
	result, err := store.saveOnlineProfile(context.Background(), in)
	if err != nil || result.CandidateID == "" {
		t.Fatalf("新在线资料未重新建档：%+v %v", result, err)
	}
	var proof string
	if err := f.db.QueryRow("SELECT proof_type FROM candidate_reopen_receipts WHERE deletion_sequence=$1", seq).Scan(&proof); err != nil || proof != "online_profile" {
		t.Fatal("未登记在线专用收据", err)
	}
	legacy := CandidateProfileInput{TenantID: f.tenant, UserEmail: in.Scope.UserEmail, PlatformID: "boss", PlatformCandidateID: in.Request.PlatformCandidateID, AutoReplyPositionID: f.pos, Phone: "13900001111", Ext: map[string]any{"structured_fields": map[string]any{}}}
	if _, err := store.SaveCandidateProfile(legacy); !errors.Is(err, errCandidateDeleted) {
		t.Fatal("缺少新双字段的旧包复活了删除来源", err)
	}
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{status}','"failed"') WHERE kind='runtime_commands' AND id='online-read'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.saveOnlineProfile(context.Background(), in); err == nil {
		t.Fatal("失败的采集命令被当作真实新在线资料")
	}
}

// TestOnlineProfileStrictAllFieldsAndUnique 验证完整条件、年龄正负3岁与唯一结果，任何缺项或冲突都不自动关联。
func TestOnlineProfileStrictAllFieldsAndUnique(t *testing.T) {
	base := OnlineCandidateProfile{CandidateName: "邓甲", EducationLevel: "大专", EarliestCompanyName: "公司", EarliestSchoolName: "学校"}
	age, work, school := 28, 2012, 2008
	base.Age, base.EarliestCompanyYear, base.EarliestSchoolYear = &age, &work, &school
	near := base
	near.CandidateName = "邓乙"
	other := 31
	near.Age = &other
	if !onlineProfilesMatch(base, near) {
		t.Fatal("同姓完整字段与正负3岁应可严格匹配")
	}
	other = 32
	if onlineProfilesMatch(base, near) {
		t.Fatal("超过3岁被关联")
	}
	missing := base
	missing.EarliestSchoolYear = nil
	if onlineProfilesMatch(base, missing) {
		t.Fatal("缺学校年份被降级关联")
	}
	conflict := base
	conflict.EducationLevel = "本科"
	if onlineProfilesMatch(base, conflict) {
		t.Fatal("最高学历冲突被关联")
	}
}
