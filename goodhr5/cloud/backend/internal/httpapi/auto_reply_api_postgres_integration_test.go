// Package httpapi 本文件使用真实 PostgreSQL 和 HTTP 请求验证自动回复权限、数据、附件、审计与邮件接口。
package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// autoReplyMailCall 记录自动回复测试发送的一封人工接管邮件。
type autoReplyMailCall struct {
	Email   string
	Subject string
	Plain   string
}

// autoReplyRecordingMailer 记录自动回复测试中的自定义邮件。
type autoReplyRecordingMailer struct {
	Calls []autoReplyMailCall
}

// SendLoginCode 忽略本测试未涉及的登录验证码邮件。
func (m *autoReplyRecordingMailer) SendLoginCode(email string, code string) error { return nil }

// SendSubscriptionReward 忽略本测试未涉及的会员邮件。
func (m *autoReplyRecordingMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	return nil
}

// SendAIBalanceNotice 忽略本测试未涉及的 AI 余额邮件。
func (m *autoReplyRecordingMailer) SendAIBalanceNotice(email string, notice AIBalanceNotice) error {
	return nil
}

// SendPositionStatus 忽略本测试未涉及的岗位状态邮件。
func (m *autoReplyRecordingMailer) SendPositionStatus(email string, notice PositionStatusNotice) error {
	return nil
}

// SendCustomHTML 记录人工接管邮件的收件人、主题和纯文本正文。
func (m *autoReplyRecordingMailer) SendCustomHTML(email string, subject string, htmlBody string, plainText string) error {
	m.Calls = append(m.Calls, autoReplyMailCall{Email: email, Subject: subject, Plain: plainText})
	return nil
}

// TestAutoReplyPostgresHTTPFlow 验证 Max、设备、岗位、会话、附件、AI审计和邮件幂等完整接口链路。
func TestAutoReplyPostgresHTTPFlow(t *testing.T) {
	dsn := os.Getenv("GOODHR_AUTO_REPLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("GOODHR_AUTO_REPLY_TEST_PG_DSN is not configured")
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalWorkingDirectory) }()
	db, err := (Config{PostgresDSN: dsn}).PostgresDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	suffix := time.Now().Format("150405.000000000")
	machineID := stableAgentMachineIDPrefix + "auto-reply-" + strings.ReplaceAll(suffix, ".", "")
	email := "auto-reply-api-" + suffix + "@example.com"
	token := "auto-reply-token-" + suffix
	authStore := NewMemoryAuthStore()
	if err = authStore.SaveSession(token, Session{Email: email, CreatedAt: time.Now()}, time.Hour); err != nil {
		t.Fatal(err)
	}
	auth := &AuthService{store: authStore}
	tenantStore := NewPostgresTenantStore(db)
	positionStore := NewPostgresPositionStore(db)
	accountStore := NewPostgresPlatformAccountStore(db)
	candidateStore := NewPostgresCandidateStore(db)
	subscriptionStore := NewPostgresSubscriptionStore(db)
	systemConfigStore := NewPostgresSystemConfigStore(db)
	agentStore := NewPostgresAgentStore(db)
	autoReplyStore := NewPostgresAutoReplyStore(db)
	mailer := &autoReplyRecordingMailer{}
	resumeDir := t.TempDir()
	service := NewAutoReplyService(
		auth, autoReplyStore, tenantStore, positionStore, accountStore, candidateStore,
		subscriptionStore, systemConfigStore, agentStore, mailer, resumeDir,
	)
	routes := autoReplyRoutesForTest(service)
	tenant, err := tenantStore.GetOrCreateTenant(email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = subscriptionStore.UserSubscription(email); err != nil {
		t.Fatal(err)
	}
	if _, err = agentStore.SaveBinding(AgentBinding{UserEmail: email, MachineID: machineID, AgentVersion: "6"}); err != nil {
		t.Fatal(err)
	}
	position, err := positionStore.SavePosition(Position{UserEmail: email, PlatformID: "liepin", Name: "猎聘企业自动回复岗位"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := accountStore.SavePlatformAccount(PlatformAccount{UserEmail: email, PlatformID: "liepin", DisplayName: "猎聘企业账号", LocalProfileID: "auto-reply-api"})
	if err != nil {
		t.Fatal(err)
	}

	companyResponse := autoReplyJSONRequestForTest(t, routes, token, "", http.MethodPost, "/api/auto-reply/company-profiles", map[string]any{
		"name": "测试公司", "address": "德阳", "contact": "邓老师", "overview": "测试公司简介",
	})
	if companyResponse.Code != http.StatusOK {
		t.Fatalf("save company status=%d body=%s", companyResponse.Code, companyResponse.Body.String())
	}
	var companyPayload struct {
		Company CompanyProfile `json:"company_profile"`
	}
	decodeAutoReplyResponseForTest(t, companyResponse, &companyPayload)
	memberEmail := "auto-reply-member-" + suffix + "@example.com"
	memberToken := "auto-reply-member-token-" + suffix
	if _, err = tenantStore.GetOrCreateTenant(memberEmail); err != nil {
		t.Fatal(err)
	}
	invitation, _, err := tenantStore.InviteMember(tenant.ID, memberEmail, "user", email)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenantStore.AcceptInvitation(invitation.ID, memberEmail); err != nil {
		t.Fatal(err)
	}
	if err = authStore.SaveSession(memberToken, Session{Email: memberEmail, CreatedAt: time.Now()}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = subscriptionStore.ReplaceSubscriptionFromNow(memberEmail, memberTypeMax, 30); err != nil {
		t.Fatal(err)
	}
	memberDeleteResponse := autoReplyJSONRequestForTest(t, routes, memberToken, "", http.MethodDelete, "/api/auto-reply/company-profiles/"+companyPayload.Company.ID, nil)
	if memberDeleteResponse.Code != http.StatusForbidden || !strings.Contains(memberDeleteResponse.Body.String(), "TEAM_ADMIN_REQUIRED") {
		t.Fatalf("member delete status=%d body=%s", memberDeleteResponse.Code, memberDeleteResponse.Body.String())
	}

	if _, err = subscriptionStore.ReplaceSubscriptionFromNow(email, memberTypePlus, 30); err != nil {
		t.Fatal(err)
	}
	plusConfigResponse := autoReplyJSONRequestForTest(t, routes, token, "", http.MethodPut, "/api/auto-reply/positions/"+position.ID+"/config", map[string]any{
		"enabled": true, "company_profile_id": companyPayload.Company.ID,
		"position_description": "本科及以上", "resume_request_message": "你好，能发一份简历吗？",
	})
	if plusConfigResponse.Code != http.StatusForbidden || !strings.Contains(plusConfigResponse.Body.String(), "AUTO_REPLY_MAX_REQUIRED") {
		t.Fatalf("plus config status=%d body=%s", plusConfigResponse.Code, plusConfigResponse.Body.String())
	}
	if _, err = subscriptionStore.ReplaceSubscriptionFromNow(email, memberTypeMax, 30); err != nil {
		t.Fatal(err)
	}
	configResponse := autoReplyJSONRequestForTest(t, routes, token, "", http.MethodPut, "/api/auto-reply/positions/"+position.ID+"/config", map[string]any{
		"enabled": true, "company_profile_id": companyPayload.Company.ID,
		"position_description": "本科及以上", "resume_request_message": "你好，能发一份简历吗？",
		"conditions": []map[string]any{{"type": "required", "content": "必须本科及以上", "enabled": true}},
	})
	if configResponse.Code != http.StatusOK {
		t.Fatalf("save config status=%d body=%s", configResponse.Code, configResponse.Body.String())
	}
	wallet := NewMemoryAIWalletStore()
	if _, err = wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: 2000, Category: "test", Reason: "自动回复启动测试"}); err != nil {
		t.Fatal(err)
	}
	execution := &PositionExecutionService{
		store: positionStore, tenantStore: tenantStore, subscriptions: subscriptionStore,
		systemConfigs: systemConfigStore, aiWallet: wallet, autoReply: autoReplyStore,
	}
	unconfiguredPosition, err := positionStore.SavePosition(Position{UserEmail: email, PlatformID: "liepin", Name: "未配置自动回复岗位"})
	if err != nil {
		t.Fatal(err)
	}
	if failure := execution.claimPositionStart(email, unconfiguredPosition, "auto_reply"); failure == nil || failure.code != "AUTO_REPLY_NOT_ENABLED" {
		t.Fatalf("unconfigured auto reply failure=%#v", failure)
	}
	if failure := execution.claimPositionStart(email, position, "auto_reply"); failure != nil {
		t.Fatalf("configured auto reply start failure=%#v", failure)
	}

	wrongDeviceResponse := autoReplyJSONRequestForTest(t, routes, token, "goodhr-device-v1-wrong", http.MethodGet, "/api/auto-reply/agent/positions/"+position.ID+"/snapshot", nil)
	if wrongDeviceResponse.Code != http.StatusForbidden {
		t.Fatalf("wrong device status=%d body=%s", wrongDeviceResponse.Code, wrongDeviceResponse.Body.String())
	}
	snapshotResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodGet, "/api/auto-reply/agent/positions/"+position.ID+"/snapshot", nil)
	if snapshotResponse.Code != http.StatusOK {
		t.Fatalf("snapshot status=%d body=%s", snapshotResponse.Code, snapshotResponse.Body.String())
	}
	positionListResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodGet, "/api/auto-reply/agent/positions?platform_id=liepin", nil)
	if positionListResponse.Code != http.StatusOK {
		t.Fatalf("position list status=%d body=%s", positionListResponse.Code, positionListResponse.Body.String())
	}
	var positionListPayload struct {
		Positions []struct {
			Position map[string]any `json:"position"`
		} `json:"positions"`
	}
	decodeAutoReplyResponseForTest(t, positionListResponse, &positionListPayload)
	if len(positionListPayload.Positions) != 1 || positionListPayload.Positions[0].Position["id"] != position.ID {
		t.Fatalf("enabled position list=%+v", positionListPayload.Positions)
	}

	candidateResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/candidates", map[string]any{
		"position_id": position.ID, "platform_id": "liepin", "platform_account_id": account.ID,
		"platform_candidate_id": "candidate-" + suffix, "candidate_name": "李女士", "gender": "女",
		"birth_ym": "1995", "birth_ym_precision": "year_estimated", "phone": "+86 176-0708-0935",
		"wechat":          "candidate_wechat",
		"education_level": "本科", "work_experiences": []any{}, "educations": []any{},
		"certificates": []any{}, "honors": []any{}, "project_experiences": []any{}, "colleague_communications": []any{},
	})
	if candidateResponse.Code != http.StatusOK {
		t.Fatalf("save candidate status=%d body=%s", candidateResponse.Code, candidateResponse.Body.String())
	}
	var candidatePayload struct {
		CandidateID string                    `json:"candidate_id"`
		Identity    CandidatePlatformIdentity `json:"platform_identity"`
	}
	decodeAutoReplyResponseForTest(t, candidateResponse, &candidatePayload)
	var savedGender, savedBirthPrecision, savedPhone, savedWechat string
	if err = db.QueryRow(`SELECT gender, birth_ym_precision, normalized_phone, wechat FROM candidate_profiles WHERE tenant_id=$1 AND id=$2`, tenant.ID, candidatePayload.CandidateID).Scan(&savedGender, &savedBirthPrecision, &savedPhone, &savedWechat); err != nil {
		t.Fatal(err)
	}
	if savedGender != "女" || savedBirthPrecision != "year_estimated" || savedPhone != "17607080935" || savedWechat != "candidate_wechat" {
		t.Fatalf("candidate normalized fields gender=%s precision=%s phone=%s wechat=%s", savedGender, savedBirthPrecision, savedPhone, savedWechat)
	}
	identityConflictResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/candidates", map[string]any{
		"position_id": position.ID, "platform_id": "liepin", "platform_account_id": account.ID,
		"platform_candidate_id": "candidate-" + suffix, "candidate_name": "李女士", "gender": "女",
		"phone": "17607080000",
	})
	if identityConflictResponse.Code != http.StatusConflict || !strings.Contains(identityConflictResponse.Body.String(), "CANDIDATE_IDENTITY_CONFLICT") {
		t.Fatalf("identity conflict status=%d body=%s", identityConflictResponse.Code, identityConflictResponse.Body.String())
	}

	conversationResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/conversations", map[string]any{
		"candidate_id": candidatePayload.CandidateID, "platform_identity_id": candidatePayload.Identity.ID,
		"position_id": position.ID, "platform_account_id": account.ID, "platform_id": "liepin",
		"platform_thread_id": "thread-" + suffix, "candidate_name": "李女士", "gender": "女",
		"page_position_text": position.Name, "status": "active",
	})
	if conversationResponse.Code != http.StatusOK {
		t.Fatalf("save conversation status=%d body=%s", conversationResponse.Code, conversationResponse.Body.String())
	}
	var conversationPayload struct {
		Conversation AutoReplyConversation `json:"conversation"`
	}
	decodeAutoReplyResponseForTest(t, conversationResponse, &conversationPayload)

	messagesBody := map[string]any{
		"conversation_id": conversationPayload.Conversation.ID, "history_complete": true,
		"messages": []map[string]any{{
			"platform_message_id": "message-" + suffix, "fingerprint": "fingerprint-" + suffix,
			"direction": "candidate", "message_type": "text", "text_content": "薪资是多少？",
		}},
	}
	firstMessageResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/messages/sync", messagesBody)
	secondMessageResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/messages/sync", messagesBody)
	if firstMessageResponse.Code != http.StatusOK || secondMessageResponse.Code != http.StatusOK || !strings.Contains(secondMessageResponse.Body.String(), `"inserted":0`) {
		t.Fatalf("message sync first=%s second=%s", firstMessageResponse.Body.String(), secondMessageResponse.Body.String())
	}

	attachmentResponse := autoReplyAttachmentRequestForTest(t, routes, token, machineID, conversationPayload.Conversation.ID, candidatePayload.CandidateID)
	if attachmentResponse.Code != http.StatusOK {
		t.Fatalf("attachment status=%d body=%s", attachmentResponse.Code, attachmentResponse.Body.String())
	}
	var attachmentPayload struct {
		Attachment StoredResumeAttachment `json:"attachment"`
	}
	decodeAutoReplyResponseForTest(t, attachmentResponse, &attachmentPayload)
	downloadResponse := autoReplyJSONRequestForTest(t, routes, token, "", http.MethodGet, "/api/auto-reply/attachments/"+attachmentPayload.Attachment.ID, nil)
	if downloadResponse.Code != http.StatusOK || !strings.HasPrefix(downloadResponse.Body.String(), "%PDF-") {
		t.Fatalf("download status=%d body=%q", downloadResponse.Code, downloadResponse.Body.String())
	}
	candidateStatePath := "/api/auto-reply/agent/candidate-state?platform_id=liepin&platform_account_id=" + account.ID +
		"&platform_candidate_id=candidate-" + suffix + "&platform_thread_id=thread-" + suffix
	candidateStateResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodGet, candidateStatePath, nil)
	if candidateStateResponse.Code != http.StatusOK || !strings.Contains(candidateStateResponse.Body.String(), `"found":true`) ||
		!strings.Contains(candidateStateResponse.Body.String(), `"has_resume_attachment":true`) ||
		!strings.Contains(candidateStateResponse.Body.String(), candidatePayload.CandidateID) {
		t.Fatalf("candidate state status=%d body=%s", candidateStateResponse.Code, candidateStateResponse.Body.String())
	}
	phoneStateResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodGet, "/api/auto-reply/agent/candidate-state?platform_id=liepin&phone=17607080935", nil)
	if phoneStateResponse.Code != http.StatusOK || !strings.Contains(phoneStateResponse.Body.String(), candidatePayload.CandidateID) {
		t.Fatalf("phone state status=%d body=%s", phoneStateResponse.Code, phoneStateResponse.Body.String())
	}

	aiStartResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/ai-runs/start", map[string]any{
		"conversation_id": conversationPayload.Conversation.ID, "candidate_id": candidatePayload.CandidateID,
		"position_id": position.ID, "trace_id": "trace-" + suffix, "model": "test-model",
		"based_on_message_key": "id:message-" + suffix,
		"input_messages":       []map[string]any{{"role": "system", "content": "自动回复测试"}},
	})
	if aiStartResponse.Code != http.StatusOK {
		t.Fatalf("start ai status=%d body=%s", aiStartResponse.Code, aiStartResponse.Body.String())
	}
	var aiPayload struct {
		Run AutoReplyAIRun `json:"ai_run"`
	}
	decodeAutoReplyResponseForTest(t, aiStartResponse, &aiPayload)
	toolResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/tool-calls", map[string]any{
		"ai_run_id": aiPayload.Run.ID, "tool_call_id": "call-1", "sequence_no": 1,
		"tool_name": "查看聊天记录", "arguments_json": map[string]any{"limit": 20},
		"result_json": map[string]any{"ok": true}, "status": "completed",
	})
	if toolResponse.Code != http.StatusOK {
		t.Fatalf("tool call status=%d body=%s", toolResponse.Code, toolResponse.Body.String())
	}
	suggestionResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/suggestions", map[string]any{
		"conversation_id": conversationPayload.Conversation.ID, "position_id": position.ID,
		"suggestion_type": "position", "operation": "update", "target_id": "salary",
		"proposed_value": map[string]any{"salary": "面议"}, "reason": "候选人询问薪资",
	})
	if suggestionResponse.Code != http.StatusOK {
		t.Fatalf("save suggestion status=%d body=%s", suggestionResponse.Code, suggestionResponse.Body.String())
	}
	var suggestionPayload struct {
		Suggestion AutoReplyConfigSuggestion `json:"suggestion"`
	}
	decodeAutoReplyResponseForTest(t, suggestionResponse, &suggestionPayload)
	listSuggestionResponse := autoReplyJSONRequestForTest(t, routes, token, "", http.MethodGet, "/api/auto-reply/suggestions?status=pending", nil)
	if listSuggestionResponse.Code != http.StatusOK || !strings.Contains(listSuggestionResponse.Body.String(), suggestionPayload.Suggestion.ID) {
		t.Fatalf("list suggestion status=%d body=%s", listSuggestionResponse.Code, listSuggestionResponse.Body.String())
	}
	reviewSuggestionResponse := autoReplyJSONRequestForTest(t, routes, token, "", http.MethodPost, "/api/auto-reply/suggestions/"+suggestionPayload.Suggestion.ID+"/review", map[string]any{"status": "approved"})
	if reviewSuggestionResponse.Code != http.StatusOK || !strings.Contains(reviewSuggestionResponse.Body.String(), `"status":"approved"`) {
		t.Fatalf("review suggestion status=%d body=%s", reviewSuggestionResponse.Code, reviewSuggestionResponse.Body.String())
	}
	aiPayload.Run.Status = "completed"
	aiPayload.Run.OutputMessage = json.RawMessage(`{"reply":"薪资面议"}`)
	aiPayload.Run.TokenUsage = 12
	aiFinishResponse := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/ai-runs/finish", aiPayload.Run)
	if aiFinishResponse.Code != http.StatusOK {
		t.Fatalf("finish ai status=%d body=%s", aiFinishResponse.Code, aiFinishResponse.Body.String())
	}
	auditResponse := autoReplyJSONRequestForTest(t, routes, token, "", http.MethodGet, "/api/auto-reply/audit?position_id="+position.ID, nil)
	if auditResponse.Code != http.StatusOK || !strings.Contains(auditResponse.Body.String(), "查看聊天记录") {
		t.Fatalf("audit status=%d body=%s", auditResponse.Code, auditResponse.Body.String())
	}

	notificationBody := map[string]any{
		"conversation_id": conversationPayload.Conversation.ID, "position_id": position.ID,
		"based_on_message_key": "id:message-" + suffix, "candidate_name": "李女士", "gender": "女",
		"platform_id": "liepin", "latest_message": "薪资是多少？", "reason": "岗位信息里没有明确薪资",
	}
	firstNotification := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/notifications", notificationBody)
	secondNotification := autoReplyJSONRequestForTest(t, routes, token, machineID, http.MethodPost, "/api/auto-reply/agent/notifications", notificationBody)
	if firstNotification.Code != http.StatusOK || secondNotification.Code != http.StatusOK || len(mailer.Calls) != 1 {
		t.Fatalf("notification first=%s second=%s calls=%d", firstNotification.Body.String(), secondNotification.Body.String(), len(mailer.Calls))
	}
	if !strings.Contains(mailer.Calls[0].Plain, position.Name) || !strings.Contains(mailer.Calls[0].Plain, "李女士") || !strings.Contains(mailer.Calls[0].Plain, "薪资是多少") {
		t.Fatalf("notification plain=%s", mailer.Calls[0].Plain)
	}
}

// autoReplyRoutesForTest 注册自动回复集成测试需要的 HTTP 路由。
func autoReplyRoutesForTest(service *AutoReplyService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auto-reply/company-profiles", service.CompanyProfiles)
	mux.HandleFunc("/api/auto-reply/company-profiles/", service.CompanyProfile)
	mux.HandleFunc("/api/auto-reply/positions/", service.Position)
	mux.HandleFunc("/api/auto-reply/agent/", service.Agent)
	mux.HandleFunc("/api/auto-reply/attachments/", service.Attachment)
	mux.HandleFunc("/api/auto-reply/audit", service.Audit)
	mux.HandleFunc("/api/auto-reply/suggestions", service.Suggestions)
	mux.HandleFunc("/api/auto-reply/suggestions/", service.Suggestion)
	return mux
}

// autoReplyJSONRequestForTest 发起带登录和可选设备头的自动回复 JSON 请求。
func autoReplyJSONRequestForTest(t *testing.T, routes http.Handler, token, machineID, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Authorization", "Bearer "+token)
	if machineID != "" {
		request.Header.Set("X-GoodHR-Machine-ID", machineID)
	}
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	return response
}

// autoReplyAttachmentRequestForTest 上传一份最小 PDF 简历附件。
func autoReplyAttachmentRequestForTest(t *testing.T, routes http.Handler, token, machineID, conversationID, candidateID string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("conversation_id", conversationID)
	_ = writer.WriteField("candidate_id", candidateID)
	_ = writer.WriteField("platform_id", "liepin")
	part, err := writer.CreateFormFile("file", "李女士简历.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte("%PDF-1.4\nGoodHR auto reply test resume")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/attachments", &body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GoodHR-Machine-ID", machineID)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	return response
}

// decodeAutoReplyResponseForTest 解析自动回复测试响应 JSON。
func decodeAutoReplyResponseForTest(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}
