// Package httpapi 本文件负责由云端读取真实简历附件、调用 AI 结构化并保存完整审计记录。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxCloudResumeStructureAttempts = 3

// cloudResumeStructureTimeout 为真实附件解析预留比通用 AI 请求更长的等待时间。
const cloudResumeStructureTimeout = 240 * time.Second

// cloudResumeStructureRequest 表示本地 Agent 发起的云端附件结构化请求。
type cloudResumeStructureRequest struct {
	ConversationID    string   `json:"conversation_id"`
	PositionID        string   `json:"position_id"`
	AttachmentIDs     []string `json:"attachment_ids"`
	CandidateName     string   `json:"candidate_name"`
	Gender            string   `json:"gender"`
	OnlineResumeText  string   `json:"online_resume_text"`
	BasedOnMessageKey string   `json:"based_on_message_key"`
}

// cloudResumeStructureInput 表示只进入动态 user 消息的附件正文和页面补充信息。
type cloudResumeStructureInput struct {
	CandidateName          string `json:"candidate_name"`
	Gender                 string `json:"gender"`
	AttachmentText         string `json:"attachment_text"`
	OnlineResumeSupplement string `json:"online_resume_supplement"`
}

// cloudResumeStructureAuditOutput 保存云端结构化的规范结果和模型原始返回。
type cloudResumeStructureAuditOutput struct {
	Candidate   cloudStructuredResume `json:"candidate"`
	RawResponse string                `json:"raw_response"`
	Attempts    int                   `json:"attempts"`
}

// agentStructureResume 验证附件归属后，由云端读取附件并完成 AI 结构化。
func (s *AutoReplyService) agentStructureResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "简历结构化这里只支持提交")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var payload cloudResumeStructureRequest
	if err := decodeAutoReplyJSON(w, r, &payload); err != nil {
		return
	}
	if err := validateCloudResumeStructureRequest(payload); err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "RESUME_STRUCTURE_INPUT_INVALID", err.Error())
		return
	}
	if _, err := s.positions.PositionByID(requestContext.Tenant.ID, requestContext.Session.Email, payload.PositionID, false); err != nil {
		writeAutoReplyStoreError(w, err, "简历对应的岗位暂时没读出来")
		return
	}
	attachmentText, attachments, err := s.readCloudResumeAttachments(r.Context(), requestContext.Tenant.ID, payload)
	if err != nil {
		writeAutoReplyInternalError(w, "RESUME_ATTACHMENT_PARSE_FAILED", "简历附件暂时没读明白，请重新试一次", err)
		return
	}
	result, rawResponse, attempts, tokenUsage, err := s.structureResumeWithAI(r.Context(), requestContext, payload, attachmentText)
	if err != nil {
		writeAutoReplyInternalError(w, "RESUME_STRUCTURE_FAILED", "AI 没把简历整理明白，我已经记下具体原因，请重新试一次", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "candidate": result, "gender": result.Gender,
		"birth_ym_precision": result.BirthYMPrecision, "wechat": result.Wechat,
		"attachment_ids": attachments, "attempts": attempts, "token_usage": tokenUsage,
		"attachment_text": attachmentText, "raw_response": rawResponse,
	})
}

// validateCloudResumeStructureRequest 校验云端结构化所需的会话、岗位、附件和消息编号。
func validateCloudResumeStructureRequest(payload cloudResumeStructureRequest) error {
	if strings.TrimSpace(payload.ConversationID) == "" || strings.TrimSpace(payload.PositionID) == "" {
		return fmt.Errorf("简历结构化缺少会话或岗位")
	}
	if len(payload.AttachmentIDs) == 0 || len(payload.AttachmentIDs) > 5 {
		return fmt.Errorf("简历结构化需要1到5个附件")
	}
	if strings.TrimSpace(payload.BasedOnMessageKey) == "" {
		return fmt.Errorf("简历结构化缺少依据消息")
	}
	return nil
}

// readCloudResumeAttachments 读取当前会话真实附件正文，并把正文回写到附件元数据。
func (s *AutoReplyService) readCloudResumeAttachments(ctx context.Context, tenantID string, payload cloudResumeStructureRequest) (string, []string, error) {
	parts := make([]string, 0, len(payload.AttachmentIDs))
	usedIDs := make([]string, 0, len(payload.AttachmentIDs))
	seen := make(map[string]struct{}, len(payload.AttachmentIDs))
	var failures []string
	for _, attachmentID := range payload.AttachmentIDs {
		attachmentID = strings.TrimSpace(attachmentID)
		if attachmentID == "" {
			continue
		}
		if _, exists := seen[attachmentID]; exists {
			continue
		}
		seen[attachmentID] = struct{}{}
		item, err := s.store.GetResumeAttachment(ctx, tenantID, attachmentID)
		if err != nil {
			return "", nil, err
		}
		if item.ConversationID != strings.TrimSpace(payload.ConversationID) {
			return "", nil, ErrAutoReplyForbidden
		}
		path, err := s.resumeStoragePath(item.StoragePath)
		if err != nil {
			return "", nil, err
		}
		text, err := extractResumeAttachmentText(path, item.MIMEType)
		if err != nil || strings.TrimSpace(text) == "" {
			failures = append(failures, fmt.Sprintf("%s：%v", item.OriginalName, firstError(err, "附件正文为空")))
			continue
		}
		if err = s.store.UpdateResumeAttachmentExtractedText(ctx, tenantID, item.ID, text); err != nil {
			return "", nil, err
		}
		parts = append(parts, "附件文件："+item.OriginalName+"\n"+text)
		usedIDs = append(usedIDs, item.ID)
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("没有从真实附件中提取到正文：%s", strings.Join(failures, "；"))
	}
	return strings.Join(parts, "\n\n"), usedIDs, nil
}

// firstError 返回可读错误；err 为空时使用 fallback。
func firstError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errors.New(fallback)
}

// structureResumeWithAI 创建审计记录，调用 AI，严格校验字段并保存完整返回。
func (s *AutoReplyService) structureResumeWithAI(ctx context.Context, requestContext autoReplyRequestContext, payload cloudResumeStructureRequest, attachmentText string) (cloudStructuredResume, string, int, int, error) {
	if s.aiConfigs == nil {
		return cloudStructuredResume{}, "", 0, 0, fmt.Errorf("云端 AI 配置存储没有准备好")
	}
	config, err := s.aiConfigs.UserConfig(requestContext.Session.Email)
	if err != nil {
		return cloudStructuredResume{}, "", 0, 0, fmt.Errorf("读取 AI 配置失败：%w", err)
	}
	if err = validateAIConfigTestRequest(aiConfigRequest{BaseURL: config.BaseURL, Model: config.Model, APIKey: config.APIKey}); err != nil {
		return cloudStructuredResume{}, "", 0, 0, fmt.Errorf("AI 配置不能用于简历结构化：%w", err)
	}
	dynamic, err := json.Marshal(cloudResumeStructureInput{
		CandidateName: strings.TrimSpace(payload.CandidateName), Gender: strings.TrimSpace(payload.Gender),
		AttachmentText: attachmentText, OnlineResumeSupplement: strings.TrimSpace(payload.OnlineResumeText),
	})
	if err != nil {
		return cloudStructuredResume{}, "", 0, 0, fmt.Errorf("编码简历结构化输入失败：%w", err)
	}
	messages := []AIMsg{{Role: "system", Content: cloudResumeStructureSystemPrompt}, {Role: "user", Content: string(dynamic)}}
	inputMessages, _ := json.Marshal(messages)
	run, err := s.store.StartAutoReplyAIRun(ctx, AutoReplyAIRun{
		TenantID: requestContext.Tenant.ID, ConversationID: strings.TrimSpace(payload.ConversationID),
		PositionID: strings.TrimSpace(payload.PositionID), TraceID: "cloud-resume-" + newAutoReplyErrorID(),
		Model: strings.TrimSpace(config.Model), BasedOnMessageKey: strings.TrimSpace(payload.BasedOnMessageKey),
		InputMessages: inputMessages,
	})
	if err != nil {
		return cloudStructuredResume{}, "", 0, 0, fmt.Errorf("创建简历结构化总记录失败：%w", err)
	}
	result, rawResponse, attempts, tokenUsage, structureErr := s.requestCloudResumeStructure(ctx, config, messages, payload)
	if structureErr != nil {
		run.Status = "failed"
		run.ErrorCode = "RESUME_STRUCTURE_FAILED"
		run.ErrorMessage = truncateAutoReplyText(structureErr.Error(), 1000)
		run.TokenUsage = tokenUsage
		run.OutputMessage = json.RawMessage(`{}`)
		_, finishErr := s.store.FinishAutoReplyAIRun(context.WithoutCancel(ctx), run)
		if finishErr != nil {
			return result, rawResponse, attempts, tokenUsage, fmt.Errorf("%w；总记录保存也失败：%v", structureErr, finishErr)
		}
		return result, rawResponse, attempts, tokenUsage, structureErr
	}
	output, err := json.Marshal(cloudResumeStructureAuditOutput{Candidate: result, RawResponse: rawResponse, Attempts: attempts})
	if err != nil {
		return result, rawResponse, attempts, tokenUsage, fmt.Errorf("编码简历结构化结果失败：%w", err)
	}
	run.Status = "completed"
	run.TokenUsage = tokenUsage
	run.OutputMessage = output
	if _, err = s.store.FinishAutoReplyAIRun(context.WithoutCancel(ctx), run); err != nil {
		return result, rawResponse, attempts, tokenUsage, fmt.Errorf("保存简历结构化总记录失败：%w", err)
	}
	return result, rawResponse, attempts, tokenUsage, nil
}

// requestCloudResumeStructure 最多修正两次字段格式，避免错误别名静默变成空经历。
func (s *AutoReplyService) requestCloudResumeStructure(ctx context.Context, config AIConfig, messages []AIMsg, payload cloudResumeStructureRequest) (cloudStructuredResume, string, int, int, error) {
	totalTokens := 0
	var lastErr error
	var rawResponse string
	for attempt := 1; attempt <= maxCloudResumeStructureAttempts; attempt++ {
		content, tokens, err := s.callCloudResumeAI(ctx, config, messages)
		totalTokens += tokens
		if err != nil {
			return cloudStructuredResume{}, rawResponse, attempt, totalTokens, err
		}
		rawResponse = content
		result, parseErr := parseCloudStructuredResume(content, payload.CandidateName, payload.Gender)
		if parseErr == nil {
			return result, rawResponse, attempt, totalTokens, nil
		}
		lastErr = parseErr
		messages = append(messages,
			AIMsg{Role: "assistant", Content: content},
			AIMsg{Role: "user", Content: "上一次字段格式不符合约定：" + truncateAutoReplyText(parseErr.Error(), 500) + "。请重新按 system 中的严格 JSON 字段返回，不要使用任何别名。"},
		)
	}
	return cloudStructuredResume{}, rawResponse, maxCloudResumeStructureAttempts, totalTokens, lastErr
}

// callCloudResumeAI 调用用户当前云端 AI 配置，并读取非流式 JSON 结果和 Token 用量。
func (s *AutoReplyService) callCloudResumeAI(ctx context.Context, config AIConfig, messages []AIMsg) (string, int, error) {
	payload, err := json.Marshal(AIRequest{
		Model: strings.TrimSpace(config.Model), Messages: messages, Temperature: 0,
		ResponseFormat: map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", 0, fmt.Errorf("编码 AI 请求失败：%w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeAIChatCompletionsURL(config.BaseURL), bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("创建 AI 请求失败：%w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(config.APIKey))
	client := s.httpClient
	if client == nil {
		client = newAIConfigTestHTTPClient()
	}
	requestClient := *client
	if requestClient.Timeout > 0 && requestClient.Timeout < cloudResumeStructureTimeout {
		requestClient.Timeout = cloudResumeStructureTimeout
	}
	response, err := requestClient.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("AI 简历结构化请求失败：%w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAICompatibleBodyBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("读取 AI 简历结构化响应失败：%w", err)
	}
	if len(body) > maxAICompatibleBodyBytes {
		return "", 0, fmt.Errorf("AI 简历结构化响应超过处理上限")
	}
	if response.StatusCode >= http.StatusBadRequest {
		return "", 0, fmt.Errorf("AI 简历结构化返回 %d：%s", response.StatusCode, truncateAutoReplyText(string(body), 500))
	}
	var decoded AIResponse
	if err = json.Unmarshal(body, &decoded); err != nil {
		return "", 0, fmt.Errorf("解析 AI 简历结构化响应失败：%w", err)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", decoded.Usage.TotalTokens, fmt.Errorf("AI 简历结构化没有返回内容")
	}
	return decoded.Choices[0].Message.Content, decoded.Usage.TotalTokens, nil
}
