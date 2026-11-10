// 本文件验证候选人自动回复摘要只返回固定公开字段，在线资料必须具有当前成员、会话和指纹证明。
package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// replySummaryJSON 构造只用于摘要投影的合成缓存和来源，隐藏字段混入时也不能通过固定DTO返回。
func replySummaryJSON(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	profile := map[string]any{"candidate_name": "测试候选人", "age": 28, "education_level": "大专", "earliest_company_name": "甲公司", "earliest_company_year": 2015, "earliest_school_name": "乙学校", "earliest_school_year": 2012, "mode": "ai", "raw_text": "HIDDEN-RAW", "screenshots": []string{"HIDDEN-IMAGE"}}
	cache := map[string]any{"candidate_id": "canonical", "profile_key": "HIDDEN-KEY", "source_key": "HIDDEN-SOURCE", "match_status": "temporary", "profile": profile}
	source := map[string]any{"actor_hash": purgeDigest("owner@example.test", canonicalReplyAccountKey("boss", "account")), "conversation_id": "thread", "profile_key": "HIDDEN-KEY", "match_status": "temporary", "captured_at": "2026-11-10T01:02:03Z", "profile": profile, "command_id": "HIDDEN-COMMAND"}
	info := map[string]any{"phone": map[string]any{"status": "received", "updated_at": "2026-11-10T01:02:04Z", "last_attempt_message_key": "HIDDEN-ANCHOR"}, "resume": map[string]any{"status": "requested"}, "foreign": map[string]any{"status": "received"}}
	encode := func(v any) []byte {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	return encode(info), encode(cache), encode(source)
}

// TestReplyHistorySummaryUsesWhitelist 验证当前联系方式、七字段、模式和进度完整，原文、图片及内部凭据不进入响应。
func TestReplyHistorySummaryUsesWhitelist(t *testing.T) {
	info, cache, source := replySummaryJSON(t)
	got := replyHistorySummary(info, cache, source, "13900001111", "candidate_wechat", "canonical", "thread", "boss", "OWNER@example.test", "account")
	if got.OnlineProfile == nil || got.OnlineProfile.Age == nil || *got.OnlineProfile.Age != 28 || got.OnlineProfile.Mode != "ai" || got.OnlineProfile.IsTemporary || got.OnlineProfile.UpdatedAt != "2026-11-10T01:02:03Z" || got.OnlineProfile.EarliestCompanyYear == nil || *got.OnlineProfile.EarliestCompanyYear != 2015 || got.OnlineProfile.EarliestSchoolName != "乙学校" {
		t.Fatal("在线七字段或当前临时状态遗漏", got)
	}
	if got.Contacts.Phone != "13900001111" || got.Contacts.Wechat != "candidate_wechat" || len(got.InfoRequests) != 2 || got.InfoRequests[0].Kind != "resume" || got.InfoRequests[1].Kind != "phone" || got.InfoRequests[1].StatusLabel != "已收到" {
		t.Fatal("联系方式或固定顺序的进度错误", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"HIDDEN", "raw_text", "screenshots", "source_key", "profile_key", "actor_hash", "last_attempt_message_key", "command_id"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatal("摘要泄露了内部字段", hidden)
		}
	}
}

// TestReplyHistorySummaryRejectsCrossScope 验证其他成员、会话、候选人及不同指纹不能借共享缓存显示在线摘要。
func TestReplyHistorySummaryRejectsCrossScope(t *testing.T) {
	info, cache, source := replySummaryJSON(t)
	for _, mode := range []string{"actor", "thread", "candidate", "fingerprint", "no_source", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			currentSource := append([]byte(nil), source...)
			currentCache := append([]byte(nil), cache...)
			email, thread, candidate := "owner@example.test", "thread", "canonical"
			switch mode {
			case "actor":
				email = "another@example.test"
			case "thread":
				thread = "another-thread"
			case "candidate":
				candidate = "another-candidate"
			case "fingerprint":
				currentSource = []byte(strings.ReplaceAll(string(source), "HIDDEN-KEY", "different"))
			case "no_source":
				currentSource = nil
			case "malformed":
				currentCache = []byte(`[]`)
			}
			got := replyHistorySummary(info, currentCache, currentSource, "", "", candidate, thread, "boss", email, "account")
			if got.OnlineProfile != nil {
				t.Fatal("不属于当前来源的在线资料进入响应", got)
			}
		})
	}
}

// TestReplyHistorySummaryEmptyAndUnknownFields 验证旧空数据有稳定空结构，缺失年龄和年份保留null而不猜数字。
func TestReplyHistorySummaryEmptyAndUnknownFields(t *testing.T) {
	empty := replyHistorySummary(nil, nil, nil, "", "", "", "", "", "", "")
	raw, err := json.Marshal(empty)
	if err != nil || string(raw) != `{"info_requests":[],"contacts":{"phone":"","wechat":""}}` {
		t.Fatal("空数据协议不稳定", string(raw), err)
	}
	info, cache, source := replySummaryJSON(t)
	var fields map[string]any
	if err = json.Unmarshal(source, &fields); err != nil {
		t.Fatal(err)
	}
	profile := fields["profile"].(map[string]any)
	delete(profile, "age")
	delete(profile, "earliest_company_year")
	delete(profile, "earliest_school_year")
	source, _ = json.Marshal(fields)
	got := replyHistorySummary(info, cache, source, "", "", "canonical", "thread", "boss", "owner@example.test", "account")
	if got.OnlineProfile == nil || !got.OnlineProfile.IsTemporary || got.OnlineProfile.Age != nil || got.OnlineProfile.EarliestCompanyYear != nil || got.OnlineProfile.EarliestSchoolYear != nil {
		t.Fatal("未知字段被猜测或临时状态丢失", got)
	}
}

// TestReplyHistoryInfoSendOnlyFormatsTechnicalReceipts 验证固定技术类型才可翻译，任意正文或不相配收据保持原文。
func TestReplyHistoryInfoSendOnlyFormatsTechnicalReceipts(t *testing.T) {
	if title, _, ok := replyHistoryInfoSend("sends", "resume-offer:offer", "接收候选人简历附件"); !ok || title != "接收简历申请" {
		t.Fatal("原生同意被显示为索要简历", title, ok)
	}
	for _, kind := range []string{"phone", "wechat", "resume"} {
		if title, description, ok := replyHistoryInfoSend("sends", "info-request:"+kind, "平台资料索要："+kind); !ok || title == "" || description == "" || strings.Contains(description, kind) {
			t.Fatal("合法技术收据没有中文名称", kind, title, description)
		}
	}
	for _, row := range [][3]string{{"messages", "info-request:phone", "平台资料索要：phone"}, {"sends", "normal-chat", "平台资料索要：phone"}, {"sends", "info-request:wechat", "平台资料索要：phone"}, {"sends", "info-request:phone", "请不要把平台资料索要：phone替换掉"}, {"sends", "resume-offer:", "接收候选人简历附件"}, {"messages", "resume-offer:offer", "接收候选人简历附件"}, {"sends", "ordinary", "接收候选人简历附件"}} {
		_, description, ok := replyHistoryInfoSend(row[0], row[1], row[2])
		if ok || description != row[2] {
			t.Fatal("普通正文被误翻译", row)
		}
	}
	for _, status := range []string{"requested", "already_requested", "received", "declined", "deferred", "unavailable", "unknown", "planned"} {
		if label := replyHistoryStatusLabel(status); label == "" || label == status || label == "状态待确认" {
			t.Fatal("资料状态未中文化", status, label)
		}
	}
}
