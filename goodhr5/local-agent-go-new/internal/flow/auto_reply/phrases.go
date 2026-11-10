// Package auto_reply 本文件根据岗位生效话术和已核实的沟通记录保护固定回复与手机号场景。
package auto_reply

import (
	"regexp"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// fixedReplyPhrase 读取岗位实际启用的完整短句。仅在旧服务未提供话术库时兼容旧签收和索要行为。
func fixedReplyPhrase(config cloud.PositionAutoReplyConfig, id string) string {
	if config.Phrases == nil && config.PhrasesVersion == "" {
		switch id {
		case "receive_resume":
			return "简历收到了，谢谢你发过来。"
		case "request_resume":
			return firstNonEmpty(config.ResumeRequestMessage, "方便发一份简历吗？我再认真看看。")
		}
	}
	for _, phrase := range config.Phrases {
		if phrase.ID == id {
			body := strings.TrimSpace(phrase.Content)
			if strings.ContainsAny(body, "{}｛｝") || len([]rune(body)) > maxAutoReplyMessageRunes {
				return ""
			}
			return body
		}
	}
	return ""
}

var mainlandChatMobile = regexp.MustCompile(`1[3-9]([ -]?[0-9]){9}`)

// phoneContactState 记录历史中已收到、已询问或候选人拒绝电话的事实；完整号码仅用于出站防回显。
type phoneContactState struct {
	Received bool `json:"received"`
	Asked    bool `json:"asked"`
	Declined bool `json:"declined"`
	numbers  []string
}

// phoneDigits 将号码中的格式分隔符去除，兼容聊天中带空格或区号的联系方式。
func phoneDigits(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, value)
}

// asksForPhone 识别已配置的索号话术及常见索号问法，以免对已经给过或明确拒绝的人重复追问。
func asksForPhone(text, configured string) bool {
	return asksForInfoInSameClause(text, configured,
		[]string{"手机号", "电话号码", "联系电话", "联系方式", "电话号", "留个电话", "留个号码", "留电话"},
		[]string{"方便", "请问", "留", "提供", "给我", "发我", "发个", "给个", "发一下", "多少", "交换", "可以", "能否", "能给", "告诉", "您的号码是"})
}

// asksForInfoInSameClause 优先识别完整配置话术，其余仅在同一自然分句同时出现资料词与请求动作时命中。
func asksForInfoInSameClause(text, configured string, subjects, actions []string) bool {
	if configured != "" && strings.Contains(text, configured) {
		return true
	}
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("。！？!?；;，,\r\n～~", r) }) {
		if containsAnyText(clause, subjects...) && containsAnyText(clause, actions...) {
			return true
		}
	}
	return false
}

// inspectPhoneContact 使用完整本机记录生成简短状态，早于最近八十条对话的拒绝也能保留。
func inspectPhoneContact(input ReplyContext) phoneContactState {
	s := phoneContactState{}
	add := func(v string) {
		n := phoneDigits(v)
		if len(n) < 7 {
			return
		}
		s.Received = true
		for _, known := range s.numbers {
			if n == known {
				return
			}
		}
		s.numbers = append(s.numbers, n)
	}
	add(input.PageSnapshot.Phone)
	if c := input.CandidateState.Candidate; c != nil {
		add(c.Phone)
	}
	if i := input.CandidateState.Identity; i != nil {
		add(i.NormalizedPhone)
	}
	configured := fixedReplyPhrase(input.Position.Config, "request_phone")
	previousRequest := false
	for _, m := range input.Messages {
		value := strings.TrimSpace(m.TextContent)
		if m.Direction == "self" {
			previousRequest = asksForPhone(value, configured)
			s.Asked = s.Asked || previousRequest
		} else if m.Direction == "candidate" {
			if (previousRequest || containsAnyText(value, "电话", "手机", "号码", "联系方式", "平台聊", "这里聊", "线上聊")) &&
				containsAnyText(value, "不方便", "不想", "不愿", "不留", "暂不", "不给", "不提供", "不要", "不需要", "不接", "只在", "只想", "只接受", "不打", "先不", "不加") {
				s.Declined = true
			}
			for _, match := range mainlandChatMobile.FindAllString(value, -1) {
				add(match)
			}
			if containsAnyText(value, "只在平台聊", "就这里聊", "先在这里聊", "这里聊吧", "平台上聊吧", "只接受线上沟通") {
				s.Declined = true
			}
			previousRequest = false
		}
	}
	return s
}

// checkPhoneReply 防止回显候选人号码或再次索号；缺资料的用户可以继续得到普通岗位回答。
func checkPhoneReply(state phoneContactState, config cloud.PositionAutoReplyConfig, reply string) error {
	digits := phoneDigits(reply)
	for _, number := range state.numbers {
		if strings.Contains(digits, number) || (strings.HasPrefix(number, "86") && len(number) == 13 && strings.Contains(digits, number[2:])) {
			return argumentError("请勿回显候选人的手机号，只需简短签收并回答本轮问题。")
		}
	}
	configured := fixedReplyPhrase(config, "request_phone")
	if asksForPhone(reply, configured) && (config.PhrasesVersion != "" && configured == "" || state.Received || state.Declined || state.Asked) {
		return argumentError("号码已获得、已询问或候选人拒绝过，不能再次索要，请继续回答岗位问题。")
	}
	return nil
}

// onlyPhoneNumber 核实这一批仅有候选人新给的一个手机号，不吞同批问题、拒绝、微信号或其他数字。
func onlyPhoneNumber(rows []cloud.AutoReplyMessage) bool {
	if len(rows) != 1 || rows[0].Direction != "candidate" || rows[0].MessageType != "text" {
		return false
	}
	text := strings.TrimSpace(rows[0].TextContent)
	for _, label := range []string{"我的手机号是", "我的手机号", "我的电话是", "我的电话", "手机号是", "手机号", "联系电话", "电话号码", "电话是", "电话"} {
		if strings.HasPrefix(text, label) {
			text = strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(text, label), "：: "))
			break
		}
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "+86"))
	return strings.TrimSpace(text) != "" && mainlandChatMobile.FindString(text) == text && len(phoneDigits(text)) == 11
}

// fixedReceiptID 只让已核实的纯卡片、纯手机号绕过模型，附带具体问题的批次照常交给 AI。
func fixedReceiptID(input ReplyContext) string {
	if canAcknowledgeResumeCard(input) {
		return "receive_resume"
	}
	if input.Position.Config.PhrasesVersion != "" && onlyPhoneNumber(input.PendingMessages) {
		return "receive_phone"
	}
	return ""
}

// previouslyReceivedPhone 用候选人旧消息核对首次提交，同号重发不重复签收。
func previouslyReceivedPhone(input ReplyContext) bool {
	if len(input.PendingMessages) != 1 {
		return false
	}
	pending := input.PendingMessages[0]
	key := firstNonEmpty(pending.PlatformMessageID, pending.Fingerprint, pending.ID)
	number := phoneDigits(mainlandChatMobile.FindString(pending.TextContent))
	if number == "" {
		return false
	}
	for _, m := range input.Messages {
		if firstNonEmpty(m.PlatformMessageID, m.Fingerprint, m.ID) == key {
			break
		}
		if m.Direction == "candidate" && onlyPhoneNumber([]cloud.AutoReplyMessage{m}) && phoneDigits(mainlandChatMobile.FindString(m.TextContent)) == number {
			return true
		}
	}
	return false
}

// handoffReplyMessages 仅在人工问题登记成功后使用；合并有据的回答与签收，必要时按原发送上限拆开。
func handoffReplyMessages(answer, receipt string) []ReplyMessage {
	answer, receipt = strings.TrimSpace(answer), strings.TrimSpace(receipt)
	if answer == receipt {
		receipt = ""
	}
	if answer != "" && receipt != "" && len([]rune(answer+"\n"+receipt)) <= maxAutoReplyMessageRunes {
		return []ReplyMessage{{Type: "answer", Content: answer + "\n" + receipt}}
	}
	var messages []ReplyMessage
	for _, text := range []string{answer, receipt} {
		if text != "" {
			messages = append(messages, ReplyMessage{Type: "answer", Content: text})
		}
	}
	return messages
}

// candidateEndedContact 避免候选人明确拒绝继续沟通时在答复后另行催简历。
func candidateEndedContact(messages []cloud.AutoReplyMessage) bool {
	for _, m := range messages {
		if m.Direction == "candidate" && containsAnyText(m.TextContent, "不考虑", "不感兴趣", "不用联系", "不要联系", "不找工作", "不方便发简历", "不想发简历", "不提供简历") {
			return true
		}
	}
	return false
}
