// 本文件通过真实HTTP认证、假发信器和并发请求验证招聘邮件权限、转义、快照及只发送一次。
package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRecruitmentMailCall 保存假邮件传输收到的参数，测试从不连接SMTP。
type fakeRecruitmentMailCall struct{ Email, ReplyTo, Subject, HTML, Body string }

// fakeRecruitmentMailer 复用开发邮件合同，仅记录招聘发送次数与模拟结果。
type fakeRecruitmentMailer struct {
	DevMailer
	mu    sync.Mutex
	calls []fakeRecruitmentMailCall
	err   error
}

// SendRecruitmentMail 记录参数后返回指定结果，不执行网络操作。
func (m *fakeRecruitmentMailer) SendRecruitmentMail(email, replyTo, subject, htmlBody, plainText string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, fakeRecruitmentMailCall{email, replyTo, subject, htmlBody, plainText})
	return m.err
}

// callCount 在锁内读取假发信次数，供并发断言使用。
func (m *fakeRecruitmentMailer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// recruitmentMailFixture 创建一个有公司署名、候选人邮箱和未来面试的独立内存服务。
func recruitmentMailFixture(t *testing.T) (*Server, http.Handler, string, RecruitmentApplication, RecruitmentMailTemplate, *fakeRecruitmentMailer) {
	t.Helper()
	server := mustNewServer(t)
	routes := server.Routes()
	owner := "recruitment-mail-owner@example.com"
	token := loginForTest(t, routes, owner)
	tenant, err := server.recruitment.tenants.GetOrCreateTenant(owner)
	if err != nil {
		t.Fatal(err)
	}
	app := RecruitmentApplication{ID: recruitmentID(), TenantID: tenant.ID, CandidateID: "mail-candidate", CandidateName: "李<script>同学", CandidateEmail: "candidate@example.com", PositionID: "mail-position", PositionName: "产品经理", OwnerEmail: owner, Status: "active", CurrentNode: 0, Revision: 1, Nodes: []RecruitmentNodeInstance{{RecruitmentNode: RecruitmentNode{ID: "node-1", Name: "初试", Type: "interview", OwnerEmail: owner}, Status: "active", Attempt: 1}}, Interviews: []RecruitmentInterview{{ID: "interview-1", NodeID: "node-1", Attempt: 1, StartsAt: time.Now().UTC().Add(24 * time.Hour), DurationMinutes: 45, Mode: "onsite", Location: "公司一楼", Status: "scheduled", Revision: 1}}}
	candidates := server.recruitment.candidates.(*MemoryCandidateStore)
	candidates.mu.Lock()
	candidates.profiles[app.CandidateID] = PositionCandidate{ID: app.CandidateID, TenantID: app.TenantID, UserEmail: owner, CandidateName: app.CandidateName, Email: app.CandidateEmail}
	candidates.mu.Unlock()
	app, err = server.recruitment.store.CreateApplication(app)
	if err != nil {
		t.Fatal(err)
	}
	company := RecruitmentCompany{CompanyName: "小树科技", ContactName: "王招聘", ContactEmail: "hr@example.com", CompanyAddress: "上海示例路1号"}
	if err = server.recruitmentMail.store.SaveCompany(tenant.ID, company); err != nil {
		t.Fatal(err)
	}
	templates, err := server.recruitmentMail.store.ListTemplates(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	var first RecruitmentMailTemplate
	for _, item := range templates {
		if item.Category == "interview_first" {
			first = item
		}
	}
	fake := &fakeRecruitmentMailer{}
	server.recruitmentMail.mailer = fake
	return server, routes, token, app, first, fake
}

// recruitmentMailAttachPreview 先请求可信预览，再将上下文指纹附到同一份待发送请求。
func recruitmentMailAttachPreview(t *testing.T, routes http.Handler, token string, input map[string]any) map[string]any {
	t.Helper()
	request := make(map[string]any)
	for _, key := range []string{"application_id", "interview_id", "template_id", "subject", "body"} {
		if value, found := input[key]; found {
			request[key] = value
		}
	}
	code, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-preview", request)
	if code != http.StatusOK {
		t.Fatalf("preview status=%d result=%v", code, response)
	}
	preview := response["preview"].(map[string]any)
	fingerprint, _ := preview["context_fingerprint"].(string)
	if len(fingerprint) != 64 {
		t.Fatalf("real preview fingerprint missing=%v", preview)
	}
	input["context_fingerprint"] = fingerprint
	return preview
}

// TestRecruitmentMailRendering 验证安全HTML、未知和缺失变量、示例标记及公司标题前缀。
func TestRecruitmentMailRendering(t *testing.T) {
	values := recruitmentMailExamples()
	values["公司名称"] = "小树科技"
	values["简历姓名"] = `<img src=x onerror=alert(1)>`
	preview := renderRecruitmentMail("[小树科技] [小树科技] {岗位名称}", "你好 {简历姓名}", values, false)
	if !preview.CanSend || strings.Count(preview.Subject, "[小树科技]") != 1 || strings.Contains(preview.HTML, "<img") || !strings.Contains(preview.HTML, "&lt;img") {
		t.Fatalf("unsafe or duplicate result=%+v", preview)
	}
	values["入职日期"] = ""
	preview = renderRecruitmentMail("{岗位名称}", "{不存在} / {入职日期}", values, false)
	if preview.CanSend || len(preview.UnknownPlaceholders) != 1 || len(preview.MissingPlaceholders) != 1 {
		t.Fatalf("missing variables=%+v", preview)
	}
	for _, test := range []struct{ name, subject, body, company string }{{"标题换行", "主题\r\nBcc: victim@example.com", "正文", "公司"}, {"公司换行", "主题", "正文", "公司\r\nBcc: victim@example.com"}, {"前缀后超长", strings.Repeat("题", 300), "正文", strings.Repeat("名", 100)}, {"待填写", "主题", "录用条件：【请填写已确认的录用条件】", "公司"}, {"花括号不配对", "主题", "{简历姓名", "公司"}} {
		t.Run(test.name, func(t *testing.T) {
			values["公司名称"] = test.company
			got := renderRecruitmentMail(test.subject, test.body, values, false)
			if got.CanSend || len(got.Warnings) == 0 {
				t.Fatalf("unsafe preview=%+v", got)
			}
		})
	}
	example := renderRecruitmentMail("{岗位名称}", "{简历姓名}", recruitmentMailExamples(), true)
	if !example.IsExample || example.CanSend {
		t.Fatalf("example=%+v", example)
	}
}

// TestRecruitmentMailHTTPConcurrency 验证同幂等编号同时提交只调用一次假SMTP且确认链接有效。
func TestRecruitmentMailHTTPConcurrency(t *testing.T) {
	server, routes, token, app, template, fake := recruitmentMailFixture(t)
	input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": "send-one"}
	recruitmentMailAttachPreview(t, routes, token, input)
	var group sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			code, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
			codes <- code
		}()
	}
	group.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent status=%d", code)
		}
	}
	if fake.callCount() != 1 {
		t.Fatalf("SMTP calls=%d", fake.callCount())
	}
	saved, found, err := server.recruitmentMail.store.FindDelivery(app.TenantID, app.OwnerEmail, "send-one")
	if err != nil || !found || saved.Status != "sent" || !saved.Prepared || saved.Recipient != "candidate@example.com" || saved.ReplyTo != "hr@example.com" {
		t.Fatalf("snapshot=%+v found=%v err=%v", saved, found, err)
	}
	if strings.Contains(saved.HTML, "<script>") || !strings.Contains(saved.HTML, "&lt;script&gt;") || !strings.Contains(saved.Body, "/interview-invitation/") {
		t.Fatalf("unsafe snapshot=%+v", saved)
	}
	memory := server.recruitment.store.(*MemoryRecruitmentStore)
	memory.mu.Lock()
	invitationCount := len(memory.invitations)
	memory.mu.Unlock()
	if invitationCount != 1 {
		t.Fatalf("duplicate confirmation tokens=%d", invitationCount)
	}
	template.Subject = "改过的模板"
	if _, err = server.recruitmentMail.store.SaveTemplate(app.TenantID, template, false); err != nil {
		t.Fatal(err)
	}
	code, result := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusOK || result["delivery"].(map[string]any)["subject"] != saved.Subject || fake.callCount() != 1 {
		t.Fatalf("replay=%d %v", code, result)
	}
	input["body"] = "改过的正文"
	code, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusConflict || fake.callCount() != 1 {
		t.Fatalf("key misuse status=%d calls=%d", code, fake.callCount())
	}
}

// TestRecruitmentMailHTTPRejectsSpoofing 验证任意收件人、未知变量、跨团队记录和未配置公司均不能发信。
func TestRecruitmentMailHTTPRejectsSpoofing(t *testing.T) {
	server, routes, token, app, template, fake := recruitmentMailFixture(t)
	for _, extra := range []map[string]any{{"email": "victim@example.com"}, {"candidate_name": "伪造姓名"}, {"context": map[string]string{"简历姓名": "伪造"}}} {
		input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": recruitmentID()}
		for k, v := range extra {
			input[k] = v
		}
		code, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
		if code != http.StatusBadRequest {
			t.Fatalf("spoof field status=%d", code)
		}
	}
	for _, body := range []string{"{不认识}", "{入职日期}", "【请填写已确认的面试结果】", "{简历姓名"} {
		input := map[string]any{"application_id": app.ID, "subject": "通知", "body": body, "idempotency_key": recruitmentID()}
		recruitmentMailAttachPreview(t, routes, token, input)
		code, result := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
		if code != http.StatusBadRequest {
			t.Fatalf("missing variable status=%d %v", code, result)
		}
	}
	other := loginForTest(t, routes, "other-mail-tenant@example.com")
	code, _ := candidateTestRequest(t, routes, other, http.MethodPost, "/api/recruitment/mail-send", map[string]any{"application_id": app.ID, "template_id": template.ID, "idempotency_key": "other-tenant"})
	if code != http.StatusNotFound {
		t.Fatalf("cross tenant status=%d", code)
	}
	if err := server.recruitmentMail.store.SaveCompany(app.TenantID, RecruitmentCompany{}); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"application_id": app.ID, "subject": "通知", "body": "你好", "idempotency_key": "no-company"}
	recruitmentMailAttachPreview(t, routes, token, input)
	code, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusBadRequest || fake.callCount() != 0 {
		t.Fatalf("company missing status=%d calls=%d", code, fake.callCount())
	}
}

// TestRecruitmentMailUncertainNeverResends 验证明确失败和结果不确定都不会因相同编号重试而重复邀请。
func TestRecruitmentMailUncertainNeverResends(t *testing.T) {
	for _, test := range []struct {
		name, status string
		err          error
	}{{"结果不确定", "uncertain", &mailDeliveryUncertainError{cause: io.EOF}}, {"明确拒绝", "failed", errors.New("SMTP refused")}} {
		t.Run(test.name, func(t *testing.T) {
			server, routes, token, app, template, fake := recruitmentMailFixture(t)
			fake.err = test.err
			input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": "result-check"}
			recruitmentMailAttachPreview(t, routes, token, input)
			for i := 0; i < 2; i++ {
				code, result := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
				if code != http.StatusOK || result["delivery"].(map[string]any)["status"] != test.status {
					t.Fatalf("status=%d response=%v", code, result)
				}
			}
			if fake.callCount() != 1 {
				t.Fatal("failed send was retried")
			}
			rows, err := server.recruitmentMail.store.ListDeliveries(app.TenantID, app.ID)
			if err != nil || len(rows) != 1 || rows[0].FinishedAt == nil {
				t.Fatalf("history=%+v err=%v", rows, err)
			}
		})
	}
}

// TestRecruitmentMailTeamMemberPermissions 验证普通成员不能维护模板或以面试官身份发送招聘邮件。
func TestRecruitmentMailTeamMemberPermissions(t *testing.T) {
	server, routes, ownerToken, app, template, fake := recruitmentMailFixture(t)
	member := "mail-interviewer@example.com"
	token := loginForTest(t, routes, member)
	invite, _, err := server.recruitment.tenants.InviteMember(app.TenantID, member, "user", app.OwnerEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.recruitment.tenants.AcceptInvitation(invite.ID, member); err != nil {
		t.Fatal(err)
	}
	if _, err = server.recruitment.store.MutateApplication(app.TenantID, app.ID, app.Revision, func(current *RecruitmentApplication) error {
		current.Nodes[0].InterviewerEmails = []string{member}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/recruitment/company", "/api/recruitment/mail-templates/" + template.ID} {
		code, _ := candidateTestRequest(t, routes, token, http.MethodPut, path, map[string]any{})
		if code != http.StatusForbidden {
			t.Fatalf("ordinary member path=%s status=%d", path, code)
		}
	}
	code, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": "interviewer-send"})
	if code != http.StatusForbidden || fake.callCount() != 0 {
		t.Fatalf("interviewer send status=%d", code)
	}
	code, _ = candidateTestRequest(t, routes, ownerToken, http.MethodPut, "/api/recruitment/company", map[string]any{"company_name": "公司\r\nBcc: bad@example.com", "contact_name": "联系人", "contact_email": "hr@example.com"})
	if code != http.StatusBadRequest {
		t.Fatalf("company CRLF status=%d", code)
	}
}

// TestRecruitmentMailTemplateIsolation 验证各团队预置模板独立且删除后不重新初始化。
func TestRecruitmentMailTemplateIsolation(t *testing.T) {
	store := NewMemoryRecruitmentMailStore()
	a, err := store.ListTemplates("a")
	if err != nil || len(a) != 7 {
		t.Fatalf("initial=%v err=%v", a, err)
	}
	b, err := store.ListTemplates("b")
	if err != nil || len(b) != 7 || a[0].ID == b[0].ID {
		t.Fatalf("shared defaults=%v", b)
	}
	if _, err = store.GetTemplate("b", a[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-team template visible")
	}
	if err = store.DeleteTemplate("a", a[0].ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := store.ListTemplates("a")
	if err != nil || len(remaining) != 6 {
		t.Fatalf("deleted builtin returned=%v", remaining)
	}
}

// TestRecruitmentReplyToAndScene 验证团队回复地址、邮件头拒绝注入及已取消场次不能再邀请。
func TestRecruitmentReplyToAndScene(t *testing.T) {
	message, err := buildRecruitmentMailMessage("platform@example.com", "candidate@example.com", "hr@example.com", "[公司] 面试", "正文", "<div>正文</div>")
	if err != nil || !strings.Contains(message, "\r\nReply-To: hr@example.com\r\n") {
		t.Fatalf("reply header=%s err=%v", message, err)
	}
	if _, err = buildRecruitmentMailMessage("platform@example.com", "candidate@example.com", "hr@example.com\r\nBcc: victim@example.com", "标题", "正文", ""); err == nil {
		t.Fatal("CRLF reply-to accepted")
	}
	ctx := RecruitmentMailContext{InterviewID: "interview", ApplicationStatus: "active", InterviewStatus: "cancelled", InterviewTime: time.Now().Add(time.Hour)}
	if validateRecruitmentMailScene("interview_first", ctx) == nil {
		t.Fatal("cancelled invitation accepted")
	}
	if validateRecruitmentMailScene("cancel", ctx) != nil {
		t.Fatal("cancelled notice blocked")
	}
}

// TestRecruitmentMailInterruptedRecovery 验证进程中断后旧sending变为不确定且相同编号永远不能再次发送。
func TestRecruitmentMailInterruptedRecovery(t *testing.T) {
	store := NewMemoryRecruitmentMailStore()
	input := RecruitmentMailDelivery{TenantID: "team", ActorEmail: "owner@example.com", ApplicationID: "app", IdempotencyKey: "interrupted", Fingerprint: "same"}
	item, claimed, err := store.ClaimDelivery(input)
	if err != nil || !claimed {
		t.Fatal(err)
	}
	store.mu.Lock()
	item.CreatedAt = time.Now().Add(-3 * time.Minute)
	store.deliveries[item.ID] = item
	store.mu.Unlock()
	recovered, found, err := store.FindDelivery("team", input.ActorEmail, input.IdempotencyKey)
	if err != nil || !found || recovered.Status != "uncertain" || recovered.FinishedAt == nil {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	_, claimed, err = store.ClaimDelivery(input)
	if err != nil || claimed {
		t.Fatalf("interrupted send reclaimed=%v err=%v", claimed, err)
	}
}

// TestRecruitmentMailPreviewUsesSendConditions 验证真实预览同步邮箱、场次和SMTP条件，且预览不创建确认令牌。
func TestRecruitmentMailPreviewUsesSendConditions(t *testing.T) {
	server, routes, token, app, template, fake := recruitmentMailFixture(t)
	input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID}
	check := func(canSend bool, warning string) {
		t.Helper()
		code, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-preview", input)
		if code != http.StatusOK {
			t.Fatalf("preview status=%d result=%v", code, response)
		}
		preview := response["preview"].(map[string]any)
		if preview["can_send"] != canSend {
			t.Fatalf("preview availability=%v", preview)
		}
		if warning != "" {
			found := false
			for _, value := range preview["warnings"].([]any) {
				if strings.Contains(value.(string), warning) {
					found = true
				}
			}
			if !found {
				t.Fatalf("warning %s missing=%v", warning, preview)
			}
		}
	}
	check(true, "")
	candidates := server.recruitment.candidates.(*MemoryCandidateStore)
	candidates.mu.Lock()
	profile := candidates.profiles[app.CandidateID]
	profile.Email = ""
	candidates.profiles[app.CandidateID] = profile
	candidates.mu.Unlock()
	check(false, "邮箱")
	input["idempotency_key"] = "no-live-email"
	recruitmentMailAttachPreview(t, routes, token, input)
	code, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusBadRequest {
		t.Fatalf("stale email snapshot accepted status=%d", code)
	}
	delete(input, "idempotency_key")
	candidates.mu.Lock()
	profile.Email = "candidate@example.com"
	candidates.profiles[app.CandidateID] = profile
	candidates.mu.Unlock()
	app, err := server.recruitment.store.MutateApplication(app.TenantID, app.ID, app.Revision, func(current *RecruitmentApplication) error {
		current.Interviews[0].Status = "cancelled"
		current.Interviews[0].Revision++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check(false, "取消")
	_, err = server.recruitment.store.MutateApplication(app.TenantID, app.ID, app.Revision, func(current *RecruitmentApplication) error {
		current.Interviews[0].Status = "scheduled"
		current.Interviews[0].Revision++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.recruitmentMail.mailer = DevMailer{}
	check(false, "平台邮件")
	if fake.callCount() != 0 {
		t.Fatal("preview sent mail")
	}
	recruitment := server.recruitment.store.(*MemoryRecruitmentStore)
	recruitment.mu.Lock()
	tokens := len(recruitment.invitations)
	recruitment.mu.Unlock()
	if tokens != 0 {
		t.Fatal("preview created invitation token")
	}
}

// TestRecruitmentMailClickableLinks 验证确认与会议链接可点击，同时拒绝脚本协议并转义恶意属性。
func TestRecruitmentMailClickableLinks(t *testing.T) {
	values := recruitmentMailExamples()
	values["公司名称"] = "公司"
	values["确认链接"] = "https://goodhr.example.com/interview-invitation/token?x=1&y=2"
	values["会议链接"] = "https://meeting.example.com/room"
	preview := renderRecruitmentMail("邀请", "<script>alert(1)</script>\n确认：{确认链接}\n会议：{会议链接}", values, false)
	if strings.Count(preview.HTML, "<a href=") != 2 || !strings.Contains(preview.HTML, `href="https://goodhr.example.com/interview-invitation/token?x=1&amp;y=2"`) || strings.Contains(preview.HTML, "<script>") || !strings.Contains(preview.Body, values["确认链接"]) {
		t.Fatalf("linked preview=%+v", preview)
	}
	for _, invalid := range []string{"javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "https://example.com/\r\nBcc: victim@example.com", "https://user:password@example.com/"} {
		values["确认链接"] = invalid
		values["会议链接"] = ""
		got := recruitmentMailLinkedHTML("确认："+invalid, values)
		if strings.Contains(got, "<a ") || strings.Contains(got, "<script>") {
			t.Fatalf("unsafe URL linked: %s", got)
		}
	}
	values["确认链接"] = `https://example.com/path?x=" onclick="alert(1)`
	got := recruitmentMailLinkedHTML("确认："+values["确认链接"], values)
	if strings.Contains(got, `href="https://example.com/path?x=" onclick=`) || strings.Contains(got, "<script>") {
		t.Fatalf("URL attribute injection=%s", got)
	}
	values["确认链接"] = "https://example.com/room/invite"
	values["会议链接"] = "https://example.com/room"
	got = recruitmentMailLinkedHTML(values["确认链接"]+"\n"+values["会议链接"], values)
	if strings.Count(got, "<a href=") != 2 || strings.Count(got, "</a>") != 2 {
		t.Fatalf("overlapping URL replacements=%s", got)
	}
}

// changingRecruitmentMailStore 只在测试中模拟最终正文保存后候选人邮箱被另一位成员修改。
type changingRecruitmentMailStore struct {
	RecruitmentMailStore
	afterPrepare func()
}

// PrepareDelivery 保存真实快照后触发测试变更，检验SMTP前最后一次可信数据核对。
func (s changingRecruitmentMailStore) PrepareDelivery(item RecruitmentMailDelivery) (RecruitmentMailDelivery, error) {
	prepared, err := s.RecruitmentMailStore.PrepareDelivery(item)
	if err == nil && s.afterPrepare != nil {
		s.afterPrepare()
	}
	return prepared, err
}

// TestRecruitmentMailCurrentContactAndDeletedHistory 验证预览与发送使用最新邮箱，删除简历后仍保留可访问的发送历史。
func TestRecruitmentMailCurrentContactAndDeletedHistory(t *testing.T) {
	server, routes, token, app, template, fake := recruitmentMailFixture(t)
	candidates := server.recruitment.candidates.(*MemoryCandidateStore)
	candidates.mu.Lock()
	profile := candidates.profiles[app.CandidateID]
	profile.Email = "updated-candidate@example.com"
	candidates.profiles[app.CandidateID] = profile
	candidates.mu.Unlock()
	input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID}
	code, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-preview", input)
	if code != http.StatusOK {
		t.Fatalf("preview=%d %v", code, response)
	}
	preview := response["preview"].(map[string]any)
	if preview["recipient"] != profile.Email || preview["reply_to"] != "hr@example.com" {
		t.Fatalf("stale preview recipient=%v", preview)
	}
	input["idempotency_key"] = "fresh-contact"
	input["context_fingerprint"] = preview["context_fingerprint"]
	code, response = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusOK || response["delivery"].(map[string]any)["recipient"] != profile.Email {
		t.Fatalf("fresh contact send=%d %v", code, response)
	}
	candidates.mu.Lock()
	delete(candidates.profiles, app.CandidateID)
	candidates.mu.Unlock()
	code, response = candidateTestRequest(t, routes, token, http.MethodGet, "/api/recruitment/mail-history?application_id="+app.ID, nil)
	if code != http.StatusOK || len(response["deliveries"].([]any)) != 1 {
		t.Fatalf("deleted candidate history=%d %v", code, response)
	}
	input["idempotency_key"] = "deleted-contact"
	code, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusBadRequest || fake.callCount() != 1 {
		t.Fatalf("deleted candidate sent=%d calls=%d", code, fake.callCount())
	}
}

// TestRecruitmentMailContactChangeBeforeSMTP 验证正文准备期间邮箱变更时停止发信，不把邮件寄到旧地址。
func TestRecruitmentMailContactChangeBeforeSMTP(t *testing.T) {
	server, routes, token, app, template, fake := recruitmentMailFixture(t)
	candidates := server.recruitment.candidates.(*MemoryCandidateStore)
	server.recruitmentMail.store = changingRecruitmentMailStore{RecruitmentMailStore: server.recruitmentMail.store, afterPrepare: func() {
		candidates.mu.Lock()
		defer candidates.mu.Unlock()
		profile := candidates.profiles[app.CandidateID]
		profile.Email = "changed-during-prepare@example.com"
		candidates.profiles[app.CandidateID] = profile
	}}
	input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": "contact-race"}
	recruitmentMailAttachPreview(t, routes, token, input)
	code, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusConflict || fake.callCount() != 0 {
		t.Fatalf("stale contact sent=%d %v calls=%d", code, response, fake.callCount())
	}
	delivery, found, err := server.recruitmentMail.store.FindDelivery(app.TenantID, app.OwnerEmail, "contact-race")
	if err != nil || !found || delivery.Status != "failed" {
		t.Fatalf("preparation failure=%+v err=%v", delivery, err)
	}
}

// TestRecruitmentMailPreviewContextChanges 验证预览后的邮箱、公司、场次和模板变化均在抢占前拒绝发送。
func TestRecruitmentMailPreviewContextChanges(t *testing.T) {
	for _, change := range []string{"email", "company", "interview", "template"} {
		t.Run(change, func(t *testing.T) {
			server, routes, token, app, template, fake := recruitmentMailFixture(t)
			input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": "preview-change"}
			recruitmentMailAttachPreview(t, routes, token, input)
			switch change {
			case "email":
				candidates := server.recruitment.candidates.(*MemoryCandidateStore)
				candidates.mu.Lock()
				profile := candidates.profiles[app.CandidateID]
				profile.Email = "new-contact@example.com"
				candidates.profiles[app.CandidateID] = profile
				candidates.mu.Unlock()
			case "company":
				company, err := server.recruitmentMail.store.GetCompany(app.TenantID)
				if err != nil {
					t.Fatal(err)
				}
				company.CompanyName = "另一家科技"
				company.ContactEmail = "new-hr@example.com"
				if err = server.recruitmentMail.store.SaveCompany(app.TenantID, company); err != nil {
					t.Fatal(err)
				}
			case "interview":
				if _, err := server.recruitment.store.MutateApplication(app.TenantID, app.ID, app.Revision, func(current *RecruitmentApplication) error {
					current.Interviews[0].StartsAt = current.Interviews[0].StartsAt.Add(time.Hour)
					current.Interviews[0].Revision++
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case "template":
				template.Subject = "新的面试邀请：{岗位名称}"
				if _, err := server.recruitmentMail.store.SaveTemplate(app.TenantID, template, false); err != nil {
					t.Fatal(err)
				}
			}
			code, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
			if code != http.StatusConflict || response["code"] != "recruitment_preview_changed" || fake.callCount() != 0 {
				t.Fatalf("changed preview accepted=%d %v calls=%d", code, response, fake.callCount())
			}
			if _, found, err := server.recruitmentMail.store.FindDelivery(app.TenantID, app.OwnerEmail, "preview-change"); err != nil || found {
				t.Fatalf("pre-preview conflict claimed delivery=%v err=%v", found, err)
			}
			recruitmentMailAttachPreview(t, routes, token, input)
			code, response = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
			if code != http.StatusOK || fake.callCount() != 1 {
				t.Fatalf("fresh preview did not send=%d %v calls=%d", code, response, fake.callCount())
			}
		})
	}
}

// TestRecruitmentMailSentReplayIgnoresContextChanges 验证已发送请求按原指纹回放，资料变化或简历删除都不会触发再次发送。
func TestRecruitmentMailSentReplayIgnoresContextChanges(t *testing.T) {
	server, routes, token, app, template, fake := recruitmentMailFixture(t)
	input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": "sent-replay"}
	recruitmentMailAttachPreview(t, routes, token, input)
	code, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusOK {
		t.Fatalf("first send=%d %v", code, response)
	}
	snapshot := response["delivery"].(map[string]any)
	candidates := server.recruitment.candidates.(*MemoryCandidateStore)
	candidates.mu.Lock()
	profile := candidates.profiles[app.CandidateID]
	profile.Email = "another-contact@example.com"
	candidates.profiles[app.CandidateID] = profile
	candidates.mu.Unlock()
	company, err := server.recruitmentMail.store.GetCompany(app.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	company.CompanyName = "新公司"
	if err = server.recruitmentMail.store.SaveCompany(app.TenantID, company); err != nil {
		t.Fatal(err)
	}
	current, err := server.recruitment.store.GetApplication(app.TenantID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.recruitment.store.MutateApplication(app.TenantID, app.ID, current.Revision, func(current *RecruitmentApplication) error {
		current.Interviews[0].StartsAt = current.Interviews[0].StartsAt.Add(time.Hour)
		current.Interviews[0].Revision++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	code, response = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusOK || response["replayed"] != true || fake.callCount() != 1 {
		t.Fatalf("original request blocked=%d %v calls=%d", code, response, fake.callCount())
	}
	replayed := response["delivery"].(map[string]any)
	if replayed["recipient"] != snapshot["recipient"] || replayed["subject"] != snapshot["subject"] || replayed["body"] != snapshot["body"] {
		t.Fatalf("replay changed snapshot=%v original=%v", replayed, snapshot)
	}
	originalFingerprint := input["context_fingerprint"]
	recruitmentMailAttachPreview(t, routes, token, input)
	code, response = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusConflict || response["code"] != "recruitment_send_key_conflict" || fake.callCount() != 1 {
		t.Fatalf("same key reused with new context=%d %v calls=%d", code, response, fake.callCount())
	}
	input["context_fingerprint"] = originalFingerprint
	candidates.mu.Lock()
	delete(candidates.profiles, app.CandidateID)
	candidates.mu.Unlock()
	code, response = candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusOK || response["replayed"] != true || fake.callCount() != 1 {
		t.Fatalf("deleted candidate replay=%d %v calls=%d", code, response, fake.callCount())
	}
}

// TestRecruitmentMailRequiresPreview 验证未预览的发送不会抢占记录，确认链接随机值不影响可信上下文指纹。
func TestRecruitmentMailRequiresPreview(t *testing.T) {
	server, routes, token, app, template, fake := recruitmentMailFixture(t)
	input := map[string]any{"application_id": app.ID, "interview_id": "interview-1", "template_id": template.ID, "idempotency_key": "missing-preview"}
	code, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/recruitment/mail-send", input)
	if code != http.StatusBadRequest || response["code"] != "recruitment_preview_required" || fake.callCount() != 0 {
		t.Fatalf("unpreviewed send=%d %v calls=%d", code, response, fake.callCount())
	}
	if _, found, err := server.recruitmentMail.store.FindDelivery(app.TenantID, app.OwnerEmail, "missing-preview"); err != nil || found {
		t.Fatalf("unpreviewed delivery=%v err=%v", found, err)
	}
	ctx := RecruitmentMailContext{ApplicationID: app.ID, CandidateEmail: "candidate@example.com", ConfirmationURL: "preview-value"}
	company := RecruitmentCompany{CompanyName: "小树科技", ContactName: "王招聘", ContactEmail: "hr@example.com"}
	fingerprint := recruitmentMailContextFingerprint(company, ctx, "标题", "{确认链接}")
	ctx.ConfirmationURL = "https://example.com/interview-invitation/random-token"
	if recruitmentMailContextFingerprint(company, ctx, "标题", "{确认链接}") != fingerprint {
		t.Fatal("random invitation token changed preview fingerprint")
	}
}
