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
	if !strings.Contains(html, "17607080935") || !strings.Contains(html, "goodhr5.58it.cn") || strings.Contains(html, "{{footer}}") || strings.Contains(html, "</html><p") {
		t.Fatalf("footer html = %s", html)
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
