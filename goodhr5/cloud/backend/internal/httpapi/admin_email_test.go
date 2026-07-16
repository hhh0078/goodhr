// 本文件负责测试超管邮件自动任务的基础工具逻辑。
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// flowReminderTestStore 提供可断言筛选条件的邮件目标测试存储。
type flowReminderTestStore struct {
	*MemoryEmailCampaignStore
	users      []EmailTargetUser
	lastFilter EmailTargetFilter
}

// automaticEmailRecordingMailer 记录自动邮件最终发送的 HTML，便于验证查看追踪图片。
type automaticEmailRecordingMailer struct {
	recordingMailer
	html string
}

// SendCustomHTML 记录自动邮件最终发送的 HTML 正文。
func (m *automaticEmailRecordingMailer) SendCustomHTML(email string, subject string, htmlBody string, plainText string) error {
	m.html = htmlBody
	return nil
}

// FindTargetUsers 返回测试准备的用户，并保存筛选条件供断言。
func (s *flowReminderTestStore) FindTargetUsers(filter EmailTargetFilter) ([]EmailTargetUser, error) {
	s.lastFilter = filter
	return s.users, nil
}

// TestNextRecoveryRun 验证自动挽回邮件会排到下一个目标小时。
func TestNextRecoveryRun(t *testing.T) {
	now := time.Date(2026, 7, 2, 8, 30, 0, 0, time.Local)
	sameDay := nextRecoveryRun(now, 9)
	if sameDay.Day() != 2 || sameDay.Hour() != 9 {
		t.Fatalf("sameDay = %v", sameDay)
	}
	nextDay := nextRecoveryRun(time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local), 9)
	if nextDay.Day() != 3 || nextDay.Hour() != 9 {
		t.Fatalf("nextDay = %v", nextDay)
	}
}

// TestAutomaticEmailTemplateHTML 验证自动邮件模板是完整移动端 HTML。
func TestAutomaticEmailTemplateHTML(t *testing.T) {
	html := automaticEmailTemplateHTML("inactive_3_days")
	if html == "" || !strings.Contains(html, "<!doctype html>") || !strings.Contains(html, "viewport") || !strings.Contains(html, "{{footer}}") {
		t.Fatalf("template html is incomplete: %q", html)
	}
}

// TestAppendEmailFooter 验证统一反馈文案会插入模板占位符。
func TestAppendEmailFooter(t *testing.T) {
	html := appendEmailFooter("<html><body>{{footer}}</body></html>", "17607080935", "https://goodhr5.58it.cn")
	if !strings.Contains(html, "联系电话") || !strings.Contains(html, "微信号") || strings.Count(html, "17607080935") != 2 || !strings.Contains(html, "goodhr5.58it.cn") || strings.Contains(html, "{{footer}}") || strings.Contains(html, "</html><p") {
		t.Fatalf("footer html = %s", html)
	}
}

// TestAppendReminderScheduleNotice 验证邮件会提前说明后续提醒日期和完成后停止提醒。
func TestAppendReminderScheduleNotice(t *testing.T) {
	registeredAt := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)
	html := appendReminderScheduleNotice("<html><body>{{footer}}</body></html>", registeredAt, 1)
	for _, expected := range []string{"2026年7月18日", "2026年7月22日", "2026年8月14日", "流程完成后不会再发送"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("提醒安排缺少 %q：%s", expected, html)
		}
	}
	if !strings.Contains(html, "{{footer}}") {
		t.Fatalf("提醒安排覆盖了页脚占位符：%s", html)
	}
	finalHTML := appendReminderScheduleNotice("<p>正文</p>", registeredAt, 30)
	if !strings.Contains(finalHTML, "最后一次") {
		t.Fatalf("30 天提醒没有说明最后一次：%s", finalHTML)
	}
}

// TestIncompleteMilestoneSourceKey 验证首日沿用旧幂等键且后续节点互不冲突。
func TestIncompleteMilestoneSourceKey(t *testing.T) {
	if got := incompleteMilestoneSourceKey("2026-07-15", 1, "agent_detected"); got != "recovery:2026-07-15:agent_detected" {
		t.Fatalf("首日幂等键错误：%s", got)
	}
	if got := incompleteMilestoneSourceKey("2026-07-13", 3, "agent_detected"); got != "recovery:2026-07-13:day-3:agent_detected" {
		t.Fatalf("三日幂等键错误：%s", got)
	}
}

// TestSendIncompleteMilestone 验证定时提醒按注册日期筛选并排除已完成用户。
func TestSendIncompleteMilestone(t *testing.T) {
	store := &flowReminderTestStore{
		MemoryEmailCampaignStore: NewMemoryEmailCampaignStore(),
		users: []EmailTargetUser{
			{Email: "pending@qq.com", Flow: UserFlowState{Stage: userFlowAgentDetected, State: "pending"}},
			{Email: "done@qq.com", Flow: UserFlowState{Stage: "completed", State: "completed"}},
		},
	}
	service := &AdminEmailService{store: store, mailer: &recordingMailer{}, systemConfigs: NewMemorySystemConfigStore()}
	result := service.sendIncompleteMilestone(3, "https://goodhr5.58it.cn")
	if len(result.Batches) != 1 || result.Batches[0].TotalCount != 1 {
		t.Fatalf("三日提醒批次错误：%+v", result)
	}
	wantDay := time.Now().AddDate(0, 0, -3).Format(time.DateOnly)
	if store.lastFilter.CreatedDay != wantDay || store.lastFilter.LastLoginExactDays != 0 {
		t.Fatalf("三日提醒筛选条件错误：%+v", store.lastFilter)
	}
}

// TestAutomaticEmailTrackingURL 验证无请求上下文的自动邮件仍使用完整公网查看地址。
func TestAutomaticEmailTrackingURL(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "")
	store := NewMemoryEmailCampaignStore()
	mailer := &automaticEmailRecordingMailer{}
	service := &AdminEmailService{store: store, mailer: mailer, systemConfigs: NewMemorySystemConfigStore()}
	baseURL := service.automaticEmailBaseURL("")
	pixel := trackingPixel(baseURL, "recipient-id")
	if baseURL != "https://goodhr5.58it.cn" || !strings.Contains(pixel, "https://goodhr5.58it.cn/api/public/mail/open?id=recipient-id") {
		t.Fatalf("自动邮件查看地址错误：baseURL=%s pixel=%s", baseURL, pixel)
	}
	batch, recipients, err := store.CreateBatch("测试自动邮件", "自动流程提醒", "tracking-test", "system", []string{"user@qq.com"})
	if err != nil {
		t.Fatal(err)
	}
	service.sendBatch(batch, recipients, "<p>正文</p>", "")
	if !strings.Contains(mailer.html, "https://goodhr5.58it.cn/api/public/mail/open?id="+recipients[0].ID) {
		t.Fatalf("自动邮件没有追加完整查看地址：%s", mailer.html)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/public/mail/open?id="+recipients[0].ID, nil)
	resp := httptest.NewRecorder()
	service.OpenPixel(resp, req)
	updated, _, err := store.GetBatch(batch.ID)
	if err != nil || updated.OpenedCount != 1 || resp.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("自动邮件查看记录没有更新：batch=%+v contentType=%s err=%v", updated, resp.Header().Get("Content-Type"), err)
	}
}

// TestFlowReminderRequestFromHTTP 验证公共接口可以通过查询参数控制流程、停留时间和预览模式。
func TestFlowReminderRequestFromHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/public/email-jobs/flow-reminder?flows=agent_detected,runtime_ready&stalled_hours=48&limit=200&dry_run=true", nil)
	parsed, err := flowReminderRequestFromHTTP(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Flows) != 2 || parsed.StalledHours != 48 || parsed.Limit != 200 || !parsed.DryRun {
		t.Fatalf("unexpected request: %+v", parsed)
	}
}

// TestSendFlowReminderPreview 验证预览按用户当前流程分组且不会创建发送批次。
func TestSendFlowReminderPreview(t *testing.T) {
	store := &flowReminderTestStore{
		MemoryEmailCampaignStore: NewMemoryEmailCampaignStore(),
		users: []EmailTargetUser{
			{Email: "agent@qq.com", Flow: UserFlowState{Stage: userFlowAgentDetected, State: "pending"}},
			{Email: "runtime@qq.com", Flow: UserFlowState{Stage: userFlowRuntimeReady, State: "blocked"}},
			{Email: "done@qq.com", Flow: UserFlowState{Stage: "completed", State: "completed"}},
		},
	}
	service := &AdminEmailService{store: store, systemConfigs: NewMemorySystemConfigStore()}
	result, err := service.sendFlowReminder(flowReminderRequest{StalledHours: 72, Limit: 100, DryRun: true}, "https://goodhr5.58it.cn")
	if err != nil {
		t.Fatal(err)
	}
	if result.Preview[userFlowAgentDetected] != 1 || result.Preview[userFlowRuntimeReady] != 1 || len(result.Batches) != 0 {
		t.Fatalf("unexpected preview: %+v", result)
	}
	if store.lastFilter.FlowInactiveHours != 72 {
		t.Fatalf("filter=%+v", store.lastFilter)
	}
}

// TestPublicFlowReminderJob 验证公共定时接口使用独立令牌鉴权并返回流程预览。
func TestPublicFlowReminderJob(t *testing.T) {
	t.Setenv("GOODHR_EMAIL_JOB_TOKEN", "flow-job-secret")
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/public/email-jobs/flow-reminder?dry_run=true", nil)
	req.Header.Set("Authorization", "Bearer flow-job-secret")
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"job":"flow-reminder"`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
