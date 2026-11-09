// Package auto_reply 本文件校验模型提议的待确认问题，从已同步的候选人本轮原话登记岗位待办。
package auto_reply

import (
	"context"
	"strings"
)

// knownRecruitmentQuestion 只向模型提供当前岗位题目、状态及已归档的公开答复，不夹带其他候选人来源。
type knownRecruitmentQuestion struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Status   string `json:"status"`
	Answer   string `json:"answer,omitempty"`
}

// handoffQuestion 表示有原文可核对的问题，来源消息键取自本轮候选人来信。
type handoffQuestion struct {
	Question           string `json:"question"`
	ExistingQuestionID string `json:"existing_question_id,omitempty"`
	sourceKey          string
}

// loadKnownQuestions 每轮从服务器查询岗位待办，失败保留本轮消息，避免用空列表重复建题。
func (f *Flow) loadKnownQuestions(ctx context.Context, input *ReplyContext) error {
	if !f.Cloud.LocalAutoReplyEnabled() || input.Position.Config.PhrasesVersion == "" {
		return nil
	}
	rows, err := f.Cloud.AutoReplyQuestionIndex(ctx, input.Credentials, input.Position.Position.ID)
	if err != nil {
		return err
	}
	input.KnownQuestions = make([]knownRecruitmentQuestion, 0, len(rows))
	for _, row := range rows {
		item := knownRecruitmentQuestion{ID: row.ID, Question: row.Question, Status: row.Status}
		if row.Status == "resolved" {
			item.Answer = row.Answer
		}
		input.KnownQuestions = append(input.KnownQuestions, item)
	}
	return nil
}

// verifiedHandoffQuestions 检查每道题都来自本批候选人原话，模型引用旧题只能使用已展示且待确认的岗位题目。
func verifiedHandoffQuestions(input ReplyContext, args notifyHRToolArgs) ([]handoffQuestion, error) {
	questions := args.Questions
	if args.Question != "" || args.ExistingQuestionID != "" {
		questions = append([]handoffQuestion{{Question: args.Question, ExistingQuestionID: args.ExistingQuestionID}}, questions...)
	}
	if args.ReasonKey != "missing_recruitment_info" {
		if len(questions) > 0 {
			return nil, argumentError("只有待核实的招聘问题可以创建岗位待办")
		}
		return nil, nil
	}
	if len(questions) == 0 && input.Position.Config.PhrasesVersion == "" {
		return nil, nil
	}
	if len(questions) == 0 || len(questions) > 8 {
		return nil, argumentError("请逐项填写1至8个缺失的招聘问题，question引用本轮原话中的问题")
	}
	seen := map[string]bool{}
	for i := range questions {
		q := &questions[i]
		q.Question = strings.TrimSpace(q.Question)
		q.ExistingQuestionID = strings.TrimSpace(q.ExistingQuestionID)
		if q.Question == "" || len([]rune(q.Question)) > 300 || seen[q.Question] || strings.Contains(q.Question, "@") || mainlandChatMobile.MatchString(q.Question) || (input.PageSnapshot.CandidateName != "" && strings.Contains(q.Question, input.PageSnapshot.CandidateName)) {
			return nil, argumentError("请引用简短且不含姓名或联系方式的问题原话，同一问题只列一次")
		}
		for _, m := range input.PendingMessages {
			if m.Direction == "candidate" && strings.Contains(m.TextContent, q.Question) {
				q.sourceKey = firstNonEmpty(m.PlatformMessageID, m.Fingerprint, m.ID)
				break
			}
		}
		if q.sourceKey == "" {
			return nil, argumentError("question 必须逐字引用本轮候选人原话中的待确认问题")
		}
		if q.ExistingQuestionID != "" {
			found := false
			for _, known := range input.KnownQuestions {
				if known.ID == q.ExistingQuestionID && known.Status != "resolved" {
					found = true
					break
				}
			}
			if !found {
				return nil, argumentError("existing_question_id 只能引用本岗位已提供的未归档问题")
			}
		}
		seen[q.Question] = true
	}
	return questions, nil
}
