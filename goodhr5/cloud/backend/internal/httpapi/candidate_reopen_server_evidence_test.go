// 本文件在隔离 PostgreSQL 中验证新附件与真实命令收据对齐旧删除代次，同时拒绝伪造、串号和过期证明。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestReopenServerProofAlignsOnlyVerifiedReplacement 覆盖同一账号规范化前后及分叉的本机删除流水号。
func TestReopenServerProofAlignsOnlyVerifiedReplacement(t *testing.T) {
	db := autoReplyStateTestDB(t)
	ctx := context.Background()
	var tenant, user, pos, candidate string
	email := "proof@example.test"
	read := func(dst any, q string, args ...any) {
		t.Helper()
		if err := db.QueryRow(q, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	read(&tenant, "INSERT INTO tenants DEFAULT VALUES RETURNING id")
	read(&user, "INSERT INTO users(tenant_id,email) VALUES($1,$2) RETURNING id", tenant, email)
	read(&pos, "INSERT INTO positions(user_id) VALUES($1) RETURNING id", user)
	read(&candidate, "INSERT INTO candidate_profiles(tenant_id,candidate_name,source_platform_id,source_platform_candidate_id) VALUES($1,'测试','liepin','candidate-key') RETURNING id", tenant)
	source := purgeSourceHash("liepin", "candidate-key")
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	boundary := cutoff.Add(2 * time.Hour)
	arrived, attached := boundary.Add(time.Minute), boundary.Add(2*time.Minute)
	oldActor := purgeDigest(email, canonicalReplyAccountKey("liepin", "郭星"))
	var seq int64
	read(&seq, "INSERT INTO candidate_deletions(tenant_id,candidate_id,row_version,owner_hash,audiences,source_hashes,deleted_at) VALUES($1,gen_random_uuid(),1,'old',jsonb_build_object($2::text,'[]'::jsonb),jsonb_build_array($3::text),$4) RETURNING sequence", tenant, purgeDigest(email), source, cutoff)
	exec("INSERT INTO candidate_reopen_receipts(deletion_sequence,source_hash,actor_hash,attachment_at,proof_hash,candidate_id) VALUES($1,$2,$3,$4,'old-proof',$5)", seq, source, oldActor, cutoff.Add(time.Minute), candidate)
	exec("INSERT INTO candidate_deletion_keys(tenant_id,kind,identity_hash,replacement_candidate_id) VALUES($1,'source',$2,$3)", tenant, source, candidate)
	exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING", tenant, user)
	put := func(kind, id string, fields map[string]any) {
		t.Helper()
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		exec("INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,$3,$4,$5,0,0,'','{}')", tenant, user, kind, id, string(raw))
	}
	put("identities", "ident", map[string]any{"candidate_id": "local", "platform_id": "liepin", "platform_account_id": "郭星", "platform_candidate_id": "candidate-key"})
	put("conversations", "conv", map[string]any{"candidate_id": "local", "platform_identity_id": "ident", "platform_id": "liepin", "platform_account_id": "郭星", "platform_thread_id": "room", "position_id": pos, "deletion_boundary": map[string]any{"source_hash": source, "sequence": seq + 1, "deleted_at": boundary}, "reopen_inbound_hash": purgeDigest("in"), "reopen_inbound_at": arrived})
	put("messages", "m", map[string]any{"conversation_id": "conv", "direction": "candidate", "platform_message_id": "in", "platform_sent_at": arrived})
	put("messages", "r", map[string]any{"conversation_id": "conv", "direction": "candidate", "fingerprint": "resume", "message_type": "resume", "platform_sent_at": attached})
	put("attachments", "pdf", map[string]any{"conversation_id": "conv", "source_message_id": "r", "relative_path": "private.pdf", "sha256": replyImportHash("pdf")})
	put("runtime_commands", "cmd", map[string]any{"action": "collect_resume", "status": "completed", "position_id": pos, "platform_thread_id": "room", "platform_account_id": "郭星", "conversation_id": "conv", "source_key": "resume", "attachment_ids": []string{"pdf"}})
	proof := candidateReopenProof{DeletionSequence: seq + 1, ConversationID: "conv", LocalCandidateID: "local", InboundMessageHash: purgeDigest("in"), InboundAt: arrived, AttachmentMessageHash: purgeDigest("resume"), AttachmentAt: attached}
	in := CandidateProfileInput{TenantID: tenant, UserEmail: email, AutoReplyPositionID: pos, PlatformID: "liepin", PlatformCandidateID: "candidate-key", ReopenPlatformAccount: "郭星", ReopenProof: &proof, ReopenServerWitness: true, Phone: "13900001111", Ext: map[string]any{"structured_fields": map[string]any{}}}
	check := func(input CandidateProfileInput, want bool) {
		t.Helper()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		receipt, _, err := verifyReopenTx(ctx, tx, tenant, source, candidate, input)
		if want {
			if err != nil || receipt.sequence != seq || receipt.actorHash != oldActor {
				t.Fatalf("新收据未沿用权威代次与同一招聘账号：%+v %v", receipt, err)
			}
		} else if !errors.Is(err, errCandidateDeleted) {
			t.Fatalf("无依据的请求被接受：%v", err)
		}
	}
	check(in, true)
	in.ReopenPlatformAccount = canonicalReplyAccountKey("liepin", "郭星")
	check(in, true)
	in.ReopenPlatformAccount = "另一账号"
	check(in, false)
	in.ReopenPlatformAccount = "郭星"
	in.ReopenServerWitness = false
	check(in, false)
	in.ReopenServerWitness = true
	exec("UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{attachment_ids}','[\"other\"]') WHERE kind='runtime_commands'")
	check(in, false)
	exec("UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{attachment_ids}','[\"pdf\"]') WHERE kind='runtime_commands'")
	key := "cloud:" + candidate
	k, _ := json.Marshal([]string{key})
	put("purge_keys", string(k), map[string]any{"entity_key": key})
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = verifyReopenTx(ctx, tx, tenant, source, candidate, in)
	tx.Rollback()
	if !errors.Is(err, errReopenTargetConflict) {
		t.Fatal("来自旧库的已删云编号不得更新", err)
	}
	// 后续真实删除的时间晚于迁入边界，即使流水号相同，也必须收紧服务器工作区。
	exec("UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{deletion_sequence}',to_jsonb($1::bigint)) WHERE kind='deletion_boundaries'", seq+1)
	fresh := attached.Add(time.Minute)
	exec("INSERT INTO candidate_deletions(tenant_id,candidate_id,row_version,owner_hash,audiences,source_hashes,deleted_at) VALUES($1,$2,1,'owner',jsonb_build_object($3::text,'[]'::jsonb),jsonb_build_array($4::text),$5)", tenant, candidate, purgeDigest(email), source, fresh)
	var stored time.Time
	read(&stored, "SELECT (payload->>'deleted_at')::timestamptz FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='deletion_boundaries'", tenant, user)
	if !stored.Equal(fresh) {
		t.Fatal("云端真实删除的截止时间没有写入编排", stored, fresh)
	}
}
