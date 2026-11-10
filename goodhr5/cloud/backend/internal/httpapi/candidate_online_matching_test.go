// 本文件在隔离PostgreSQL核验在线唯一匹配、可见范围和人工资料保护，不访问真实简历库。
package httpapi

import (
	"context"
	"sync"
	"testing"
	"time"
)

// createOnlineCanonical 创建一份已有正式资料，供验证新来源只关联、不覆盖人工内容。
func createOnlineCanonical(t *testing.T, f editableFactFixture, p OnlineCandidateProfile, phone, owner string) PositionCandidate {
	t.Helper()
	if owner != "fact-hr@example.test" {
		if _, err := f.db.Exec("INSERT INTO users(tenant_id,email) VALUES($1,$2) ON CONFLICT(email) DO NOTHING", f.tenant, owner); err != nil {
			t.Fatal(err)
		}
	}
	fields := map[string]any{"candidate_name": "邓明", "phone": phone, "age": "28", "education_level": p.EducationLevel, "work_experiences": p.WorkExperiences, "educations": p.Educations, "raw_text": "人工维护的正式原文", "basic_info": "人工维护的基础资料", "platform_id": "manual", "platform_candidate_id": "manual-" + phone, "source_type": "manual"}
	item, err := NewPostgresCandidateStore(f.db).SaveLibraryCandidate(CandidateLibraryScope{TenantID: f.tenant, UserEmail: owner, IsAdmin: true}, "", fields, 0, "online-canonical-"+phone)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestOnlineProfilePGConcurrentSourcesShareOneCanonical 验证两个平台来源并发完成时，完整唯一匹配仍只创建一个主体。
func TestOnlineProfilePGConcurrentSourcesShareOneCanonical(t *testing.T) {
	f, store, first := onlineProfileFixture(t)
	second := first
	second.Request = first.Request
	second.Request.ConversationID = "online-thread-two"
	second.Request.PlatformCandidateID = "754299373"
	second.Request.Profile.SourceKey = "online-image:second-synthetic-source"
	arrived := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	f.entity(t, "candidates", "online-local-two", map[string]any{"ID": "online-local-two", "CandidateName": second.Request.Profile.CandidateName})
	f.entity(t, "identities", "online-identity-two", map[string]any{"candidate_id": "online-local-two", "platform_id": "boss", "platform_account_id": "郭星", "platform_candidate_id": second.Request.PlatformCandidateID})
	f.entity(t, "conversations", second.Request.ConversationID, map[string]any{"candidate_id": "online-local-two", "platform_identity_id": "online-identity-two", "position_id": f.pos, "platform_id": "boss", "platform_account_id": "郭星", "platform_thread_id": "754299373-0", "candidate_name": second.Request.Profile.CandidateName})
	f.entity(t, "messages", "online-inbound-two", map[string]any{"conversation_id": second.Request.ConversationID, "direction": "candidate", "fingerprint": "new-question-two", "platform_sent_at": arrived})
	f.entity(t, "runtime_commands", "online-read-two", map[string]any{"action": "read_online_profile", "status": "completed", "position_id": f.pos, "machine_id": second.MachineID, "platform_account_id": "郭星", "platform_thread_id": "754299373-0", "conversation_id": second.Request.ConversationID, "source_key": second.Request.Profile.SourceKey, "updated_at": arrived.Add(time.Minute)})
	var wait sync.WaitGroup
	results := make([]autoReplyOnlineProfileResult, 2)
	errs := make([]error, 2)
	for i, in := range []onlineProfileStoreInput{first, second} {
		wait.Add(1)
		go func(index int, input onlineProfileStoreInput) {
			defer wait.Done()
			results[index], errs[index] = store.saveOnlineProfile(context.Background(), input)
		}(i, in)
	}
	wait.Wait()
	if errs[0] != nil || errs[1] != nil || results[0].CandidateID == "" || results[0].CandidateID != results[1].CandidateID {
		t.Fatalf("并发来源创建了重复主体：%+v %v", results, errs)
	}
	var profiles, sources int
	if err := f.db.QueryRow("SELECT count(*) FROM candidate_profiles WHERE tenant_id=$1", f.tenant).Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("SELECT count(*) FROM candidate_profile_sources WHERE tenant_id=$1", f.tenant).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if profiles != 1 || sources != 2 {
		t.Fatalf("并发主体/来源数量错误：%d/%d", profiles, sources)
	}
}

// TestOnlineProfilePGStrictUniqueAndVisibility 验证完整唯一才关联，同姓多条或不可见资料不会自动合并。
func TestOnlineProfilePGStrictUniqueAndVisibility(t *testing.T) {
	for _, kind := range []string{"unique", "ambiguous", "missing", "hidden", "foreign", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			f, store, in := onlineProfileFixture(t)
			owner := "fact-hr@example.test"
			if kind == "hidden" {
				owner = "peer-hr@example.test"
				in.Scope.IsAdmin = false
			}
			baseFixture := f
			if kind == "foreign" {
				if err := f.db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&baseFixture.tenant); err != nil {
					t.Fatal(err)
				}
				owner = "foreign-hr@example.test"
			}
			old := createOnlineCanonical(t, baseFixture, in.Request.Profile, "13800001111", owner)
			if kind == "ambiguous" {
				createOnlineCanonical(t, f, in.Request.Profile, "13800002222", owner)
			}
			if kind == "missing" {
				in.Request.Profile.EarliestSchoolYear = nil
			}
			if kind == "conflict" {
				in.Request.Profile.EducationLevel = "本科"
			}
			result, err := store.saveOnlineProfile(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "unique" {
				if result.CandidateID != old.ID || result.MatchStatus != "matched" {
					t.Fatalf("唯一完整条件未关联：%+v", result)
				}
				current, err := store.GetPositionCandidate(f.tenant, old.ID, "", owner, true)
				if err != nil || current.CandidateName != old.CandidateName || current.RawText != old.RawText {
					t.Fatalf("在线来源覆盖了人工正式资料：%+v %v", current, err)
				}
			} else if result.CandidateID == old.ID || result.CandidateID == "" {
				t.Fatalf("%s错误关联或未保留独立资料：%+v", kind, result)
			}
			if kind == "ambiguous" && result.MatchStatus != "ambiguous" {
				t.Fatalf("多个匹配未明确待人工处理：%+v", result)
			}
		})
	}
}

// TestOnlineProfilePGNativeSourceCannotAccessHiddenOwner 验证原生ID也不能越过当前成员的简历可见范围。
func TestOnlineProfilePGNativeSourceCannotAccessHiddenOwner(t *testing.T) {
	f, store, in := onlineProfileFixture(t)
	old := createOnlineCanonical(t, f, in.Request.Profile, "13800001111", "peer-hr@example.test")
	if _, err := f.db.Exec(`UPDATE candidate_profiles SET source_platform_id='boss',source_platform_candidate_id=$1 WHERE id=$2`, in.Request.PlatformCandidateID, old.ID); err != nil {
		t.Fatal(err)
	}
	in.Scope.IsAdmin = false
	if _, err := store.saveOnlineProfile(context.Background(), in); err == nil {
		t.Fatal("原生ID关联越过了成员可见范围")
	}
}

// TestOnlineProfilePGCanonicalAgeDoesNotDrift 验证31岁来源关联28岁主档后仍按28岁判断，不由来源逐次放宽到34岁。
func TestOnlineProfilePGCanonicalAgeDoesNotDrift(t *testing.T) {
	f, store, in := onlineProfileFixture(t)
	old := createOnlineCanonical(t, f, in.Request.Profile, "13800001111", in.Scope.UserEmail)
	near := 31
	in.Request.Profile.Age = &near
	result, err := store.saveOnlineProfile(context.Background(), in)
	if err != nil || result.CandidateID != old.ID {
		t.Fatalf("年龄边界来源未关联：%+v %v", result, err)
	}
	current, err := store.GetPositionCandidate(f.tenant, old.ID, "", in.Scope.UserEmail, true)
	if err != nil || onlineProfileFromCandidate(current).Age == nil || *onlineProfileFromCandidate(current).Age != 28 {
		t.Fatalf("来源覆盖了主体年龄：%+v %v", current, err)
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, age := range []int{34, 25} {
		profile := in.Request.Profile
		profile.Age = &age
		ids, err := strictOnlineCandidates(context.Background(), tx, in.Scope, profile)
		if err != nil || (age == 34 && len(ids) != 0) || (age == 25 && (len(ids) != 1 || ids[0] != old.ID)) {
			t.Fatalf("年龄%d没有以主体28岁判断：%v %v", age, ids, err)
		}
	}
}
