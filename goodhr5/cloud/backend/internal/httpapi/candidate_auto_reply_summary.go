// 本文件把已授权会话的在线资料、资料索要和联系方式投影为固定展示字段，不返回截图、原始命令或来源凭据。
package httpapi

import (
	"encoding/json"
	"strings"
	"time"
)

// replyThreadSummary 为当前会话和会话列表共用同一摘要，禁止把不同成员或会话的状态拼接在一起。
type replyThreadSummary struct {
	OnlineProfile *replyOnlineProfileView `json:"online_profile,omitempty"`
	InfoRequests  []replyInfoRequestView  `json:"info_requests"`
	Contacts      replyContactsView       `json:"contacts"`
}

// replyOnlineProfileView 只公开七项基础信息及识别、关联和临时状态，未知年龄及年份保留null。
type replyOnlineProfileView struct {
	CandidateName       string `json:"candidate_name"`
	Age                 *int   `json:"age"`
	EducationLevel      string `json:"education_level"`
	EarliestCompanyName string `json:"earliest_company_name"`
	EarliestCompanyYear *int   `json:"earliest_company_year"`
	EarliestSchoolName  string `json:"earliest_school_name"`
	EarliestSchoolYear  *int   `json:"earliest_school_year"`
	Mode                string `json:"mode"`
	MatchStatus         string `json:"match_status"`
	IsTemporary         bool   `json:"is_temporary"`
	UpdatedAt           string `json:"updated_at"`
}

// replyInfoRequestView 不向网页返回内部消息锚点，只展示三种明确资料动作的当前进度。
type replyInfoRequestView struct {
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	StatusLabel string `json:"status_label"`
	UpdatedAt   string `json:"updated_at"`
}

// replyContactsView 来自当前成员已绑定的候选人实体，不从其他成员的共用简历补联系方式。
type replyContactsView struct {
	Phone  string `json:"phone"`
	Wechat string `json:"wechat"`
}

// emptyReplyThreadSummary 让旧记录和没有会话的响应保持可遍历的空列表及固定联系方式字段。
func emptyReplyThreadSummary() replyThreadSummary {
	return replyThreadSummary{InfoRequests: []replyInfoRequestView{}}
}

// replyHistorySummary 复核在线来源的当前成员、会话和资料指纹，不能把候选人共用缓存误当成每个会话都读取过。
func replyHistorySummary(info, online, source []byte, phone, wechat, candidate, thread, platform, email, account string) replyThreadSummary {
	out := emptyReplyThreadSummary()
	out.Contacts = replyContactsView{Phone: phone, Wechat: wechat}
	var progress map[string]struct {
		Status    string `json:"status"`
		UpdatedAt string `json:"updated_at"`
	}
	if json.Unmarshal(info, &progress) == nil {
		for _, kind := range []string{"resume", "phone", "wechat"} {
			if value, found := progress[kind]; found {
				out.InfoRequests = append(out.InfoRequests, replyInfoRequestView{Kind: kind, Status: value.Status, StatusLabel: replyHistoryStatusLabel(value.Status), UpdatedAt: value.UpdatedAt})
			}
		}
	}
	var cache struct {
		CandidateID string                 `json:"candidate_id"`
		ProfileKey  string                 `json:"profile_key"`
		MatchStatus string                 `json:"match_status"`
		Profile     replyOnlineProfileView `json:"profile"`
	}
	var proof struct {
		ActorHash      string                 `json:"actor_hash"`
		ConversationID string                 `json:"conversation_id"`
		ProfileKey     string                 `json:"profile_key"`
		MatchStatus    string                 `json:"match_status"`
		CapturedAt     time.Time              `json:"captured_at"`
		Profile        replyOnlineProfileView `json:"profile"`
	}
	if json.Unmarshal(online, &cache) != nil || json.Unmarshal(source, &proof) != nil || candidate == "" || cache.CandidateID != candidate || cache.ProfileKey == "" || cache.ProfileKey != proof.ProfileKey || proof.ConversationID != thread || proof.ActorHash != purgeDigest(strings.ToLower(email), canonicalReplyAccountKey(platform, account)) {
		return out
	}
	p := proof.Profile
	if strings.TrimSpace(p.CandidateName) == "" {
		return out
	}
	p.Mode = ""
	if cache.Profile.Mode == "ai" || cache.Profile.Mode == "dom" {
		p.Mode = cache.Profile.Mode
	}
	p.MatchStatus = cache.MatchStatus
	if p.MatchStatus == "" {
		p.MatchStatus = proof.MatchStatus
	}
	p.IsTemporary = normalizeCandidatePhone(phone) == ""
	if !proof.CapturedAt.IsZero() {
		p.UpdatedAt = proof.CapturedAt.UTC().Format(time.RFC3339Nano)
	}
	out.OnlineProfile = &p
	return out
}

// replyHistoryStatusLabel 将服务器技术状态转换成中文，保留原状态字段供筛选及旧客户端继续使用。
func replyHistoryStatusLabel(status string) string {
	labels := map[string]string{
		"planned": "待申请", "requested": "已申请", "already_requested": "已申请", "received": "已收到", "declined": "候选人暂不提供",
		"deferred": "暂缓申请", "unavailable": "暂不可用", "unknown": "结果待核实", "pending": "等待中", "waiting": "等待中",
		"running": "进行中", "processing": "处理中", "in_progress": "进行中", "executing": "执行中", "active": "沟通中",
		"completed": "已完成", "done": "已完成", "sent": "已发送", "success": "成功", "succeeded": "成功", "result": "已完成",
		"failed": "失败", "error": "失败", "blocked": "受阻", "stopped": "已停止", "skipped": "已跳过", "cancelled": "已取消",
		"waiting_candidate": "等待候选人", "waiting_hr": "等待 HR", "waiting_resume": "等待简历", "awaiting_hr": "等待 HR 补充",
		"matched": "已满足", "unmet": "不满足", "unmatched": "不满足", "ready": "准备就绪", "loading": "处理中",
	}
	if label := labels[status]; label != "" {
		return label
	}
	return "状态待确认"
}

// replyHistoryInfoSend 仅翻译已有技术发送种类与固定内容同时吻合的收据，任意候选人或HR聊天正文不会被替换。
func replyHistoryInfoSend(kind, key, content string) (string, string, bool) {
	if kind == "sends" {
		if strings.HasPrefix(key, "resume-offer:") && strings.TrimPrefix(key, "resume-offer:") != "" && content == "接收候选人简历附件" {
			return "接收简历申请", content, true
		}
		for technical, label := range map[string]string{"resume": "简历", "phone": "手机号", "wechat": "微信"} {
			if key == "info-request:"+technical && content == "平台资料索要："+technical {
				return "索要" + label + "收据", "通过平台索要" + label, true
			}
		}
	}
	return "", content, false
}
