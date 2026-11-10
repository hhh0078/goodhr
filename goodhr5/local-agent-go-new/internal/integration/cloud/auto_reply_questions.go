// 本文件为服务器流程读取 PostgreSQL 岗位待确认问题，并为真实来源来信登记问题。
package cloud

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// AutoReplyQuestion 保存岗位相关待确认事项；Sources 只在本机管理列表中即时填充。
type AutoReplyQuestion struct {
	ID             string                    `json:"id"`
	PositionID     string                    `json:"position_id"`
	Question       string                    `json:"question"`
	Answer         string                    `json:"answer"`
	Status         string                    `json:"status"`
	PublishTarget  string                    `json:"publish_target"`
	Error          string                    `json:"error"`
	Version        int                       `json:"version"`
	UpdatedAt      time.Time                 `json:"updated_at"`
	SourceMessages []AutoReplyQuestionSource `json:"source_messages,omitempty"`
}

// AutoReplyQuestionSource 展示真实来信；不随已确认事实上传。
type AutoReplyQuestionSource struct {
	MessageID     string `json:"message_id"`
	Text          string `json:"text"`
	CandidateName string `json:"candidate_name"`
}

// AutoReplyQuestions 为自动回复模型仅读取岗位题义、状态和已确认的公开事实。
func (c *Client) AutoReplyQuestions(ctx context.Context, credentials AgentCredentials, positionID string) ([]AutoReplyQuestion, error) {
	return c.AutoReplyQuestionIndex(ctx, credentials, positionID)
}

// AutoReplyQuestionIndex 不向本轮模型传入其他候选人的原话或未归档的HR答复。
func (c *Client) AutoReplyQuestionIndex(ctx context.Context, credentials AgentCredentials, positionID string) ([]AutoReplyQuestion, error) {
	var out struct {
		Questions []AutoReplyQuestion `json:"questions"`
	}
	err := c.doWithMachineID(ctx, http.MethodGet, "/api/auto-reply/positions/"+url.PathEscape(positionID)+"/questions?scope=index", credentials.Token, credentials.MachineID, nil, &out)
	return out.Questions, err
}

// RecordAutoReplyQuestion 由服务器流程引用已同步的候选人来信登记或合并同义问题。
func (c *Client) RecordAutoReplyQuestion(ctx context.Context, credentials AgentCredentials, positionID, existingQuestionID, question, conversationID, messageKey string) (AutoReplyQuestion, error) {
	var out struct {
		Question AutoReplyQuestion `json:"question"`
	}
	err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/questions/record", credentials.Token, credentials.MachineID, map[string]string{"position_id": positionID, "existing_question_id": existingQuestionID, "question": question, "conversation_id": conversationID, "message_key": messageKey}, &out)
	return out.Question, err
}
