// 本文件在独立PostgreSQL验证在线建档时补齐先前真实来信时间，查看及重放不延期、较新沟通不回退、无合法时间不伪造。
package httpapi

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestOnlineProfilePGSeedsInitialCommunication 验证消息先到、主档后建或关联时均补实际平台时间，重放不更新资料时间或版本。
func TestOnlineProfilePGSeedsInitialCommunication(t *testing.T) {
	for _, scenario := range []string{"new", "matched", "newer-preserved"} {
		t.Run(scenario, func(t *testing.T) {
			f, store, in := onlineProfileFixture(t)
			var inbound time.Time
			if err := f.db.QueryRow(`SELECT (payload->>'platform_sent_at')::timestamptz FROM auto_reply_cloud_entities WHERE kind='messages' AND id='online-inbound'`).Scan(&inbound); err != nil {
				t.Fatal(err)
			}
			expected := inbound
			oldID := ""
			if scenario != "new" {
				old := createOnlineCanonical(t, f, in.Request.Profile, "13800001111", in.Scope.UserEmail)
				oldID = old.ID
				if scenario == "newer-preserved" {
					expected = inbound.Add(time.Minute)
					if _, err := f.db.Exec(`UPDATE candidate_profiles SET last_communication_at=$2 WHERE id=$1`, oldID, expected); err != nil {
						t.Fatal(err)
					}
				}
			}
			result, err := store.saveOnlineProfile(context.Background(), in)
			if err != nil || result.CandidateID == "" || oldID != "" && result.CandidateID != oldID {
				t.Fatalf("在线资料没有建立或关联主档：%+v %v", result, err)
			}
			var contact, updated, sourceUpdated time.Time
			var version int64
			if err := f.db.QueryRow(`SELECT cp.last_communication_at,cp.updated_at,cp.row_version,cs.updated_at FROM candidate_profiles cp JOIN candidate_profile_sources cs ON cs.tenant_id=cp.tenant_id AND cs.candidate_id=cp.id AND cs.platform_id='boss' WHERE cp.id=$1`, result.CandidateID).Scan(&contact, &updated, &version, &sourceUpdated); err != nil || !contact.Equal(expected) {
				t.Fatalf("初始沟通被丢失、写成now或回退：actual=%v expected=%v err=%v", contact, expected, err)
			}
			if _, err = store.saveOnlineProfile(context.Background(), in); err != nil {
				t.Fatal(err)
			}
			var againContact, againUpdated, againSource time.Time
			var againVersion int64
			if err := f.db.QueryRow(`SELECT cp.last_communication_at,cp.updated_at,cp.row_version,cs.updated_at FROM candidate_profiles cp JOIN candidate_profile_sources cs ON cs.tenant_id=cp.tenant_id AND cs.candidate_id=cp.id AND cs.platform_id='boss' WHERE cp.id=$1`, result.CandidateID).Scan(&againContact, &againUpdated, &againVersion, &againSource); err != nil || !againContact.Equal(expected) || !againUpdated.Equal(updated) || !againSource.Equal(sourceUpdated) || againVersion != version {
				t.Fatalf("查看或重复保存错误延长沟通/资料保留期：%v", err)
			}
		})
	}
}

// TestOnlineProfilePGDoesNotInventCommunication 验证缺失、零值及未来平台时间不能用保存时刻补成真实沟通，旧主档保持空。
func TestOnlineProfilePGDoesNotInventCommunication(t *testing.T) {
	for _, scenario := range []string{"missing", "zero", "future"} {
		t.Run(scenario, func(t *testing.T) {
			f, store, in := onlineProfileFixture(t)
			old := createOnlineCanonical(t, f, in.Request.Profile, "13800001111", in.Scope.UserEmail)
			var err error
			if scenario == "missing" {
				_, err = f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=payload-'platform_sent_at' WHERE kind='messages'`)
			} else {
				at := time.Time{}
				if scenario == "future" {
					at = time.Now().UTC().Add(time.Hour)
				}
				_, err = f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{platform_sent_at}',to_jsonb($1::text)) WHERE kind='messages'`, at.Format(time.RFC3339Nano))
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.saveOnlineProfile(context.Background(), in); err == nil {
				t.Fatal("无合法真实消息仍被当成有效在线来信")
			}
			var contact sql.NullTime
			if err = f.db.QueryRow(`SELECT last_communication_at FROM candidate_profiles WHERE id=$1`, old.ID).Scan(&contact); err != nil || contact.Valid {
				t.Fatalf("无合法时间时伪造了沟通：%+v %v", contact, err)
			}
		})
	}
}
