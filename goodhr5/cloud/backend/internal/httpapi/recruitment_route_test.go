// 本文件通过真实服务装配验证企业招聘路由、登录边界和插件最小权限，不发送真实邮件。
package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRecruitmentNodeOwnerCannotAssignWholeProcess 验证当前节点负责人能推进但不能冒充申请负责人调整节点人员。
func TestRecruitmentNodeOwnerCannotAssignWholeProcess(t *testing.T) {
	service, owner, _ := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, service, owner, "all")
	workflow.Nodes[0].OwnerEmail = "one@example.com"
	workflow, err := service.store.SaveWorkflow(owner.TenantID, workflow, workflow.Revision)
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.StartApplication(owner, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	nodeOwner := CandidateLibraryScope{TenantID: owner.TenantID, UserEmail: "one@example.com"}
	read, err := service.GetApplication(nodeOwner, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !read.Permissions.CanManage || !read.Permissions.CanAdvance || read.Permissions.CanAssign {
		t.Fatalf("node owner permissions=%+v", read.Permissions)
	}
	_, err = service.Act(nodeOwner, app.ID, RecruitmentAction{Action: "assign", Revision: read.Revision, NodeID: read.Nodes[0].ID, OwnerEmail: nodeOwner.UserEmail})
	if !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("node owner assignment err=%v", err)
	}
	ownerPermissions := recruitmentPermissions(owner, app)
	if !ownerPermissions.CanAssign {
		t.Fatal("application owner cannot assign")
	}
	app.Status = "completed"
	if recruitmentPermissions(owner, app).CanAssign {
		t.Fatal("ended application still offers assignment")
	}
}

// TestRecruitmentReturnNotifiesTargetCC 验证退回后目标节点的抄送人重新收到未读进度，不遗漏重新进入节点。
func TestRecruitmentReturnNotifiesTargetCC(t *testing.T) {
	service, owner, _ := recruitmentFixture(t)
	workflow := defaultRecruitmentWorkflow(owner.TenantID)
	workflow.Nodes = []RecruitmentNode{
		{ID: "first-review", Name: "初筛", Type: "review", OwnerEmail: owner.UserEmail, CCEmails: []string{"one@example.com"}, ApprovalMode: "all"},
		{ID: "second-review", Name: "复核", Type: "review", OwnerEmail: owner.UserEmail, CCEmails: []string{"two@example.com"}, ApprovalMode: "all"},
	}
	workflow, err := service.store.SaveWorkflow(owner.TenantID, workflow, 0)
	if err != nil {
		t.Fatal(err)
	}
	app, err := service.StartApplication(owner, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	app = recruitmentTestAct(t, service, owner, app, RecruitmentAction{Action: "advance"})
	cc := CandidateLibraryScope{TenantID: owner.TenantID, UserEmail: "one@example.com"}
	app = recruitmentTestAct(t, service, cc, app, RecruitmentAction{Action: "read_notifications"})
	if recruitmentPendingFor(cc, app) {
		t.Fatal("read CC still has a pending task")
	}
	app = recruitmentTestAct(t, service, owner, app, RecruitmentAction{Action: "return", TargetNodeIndex: 0, Note: "重新核对简历"})
	if !recruitmentPendingFor(cc, app) {
		t.Fatal("returned node CC did not receive a new notification")
	}
	event := app.Events[len(app.Events)-1]
	if event.Type != "node_entered" || event.NodeID != "first-review" || !recruitmentContains(event.NotifyEmails, cc.UserEmail) {
		t.Fatalf("target notification=%+v", event)
	}
}

// TestRecruitmentServerRoutesRequireWebSession 验证所有私有招聘入口均注册并先检查登录。
func TestRecruitmentServerRoutesRequireWebSession(t *testing.T) {
	t.Setenv("GOODHR_PG_DSN", "")
	t.Setenv("GOODHR_REDIS_ADDR", "")
	t.Setenv("GOODHR_SMTP_HOST", "")
	server := mustNewServer(t)
	routes := server.Routes()
	for _, item := range []struct{ method, path string }{
		{http.MethodGet, "/api/recruitment/company"},
		{http.MethodGet, "/api/recruitment/workflows"},
		{http.MethodGet, "/api/recruitment/applications"},
		{http.MethodGet, "/api/recruitment/mail-templates"},
		{http.MethodGet, "/api/recruitment/placeholders"},
		{http.MethodGet, "/api/recruitment/mail-history"},
		{http.MethodPost, "/api/recruitment/mail-preview"},
		{http.MethodPost, "/api/recruitment/mail-send"},
		{http.MethodPost, "/api/recruitment/applications/missing/actions"},
	} {
		r := httptest.NewRequest(item.method, item.path, strings.NewReader("{}"))
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status=%d body=%s", item.method, item.path, w.Code, w.Body.String())
		}
	}
}

// TestRecruitmentServerRejectsExtensionSession 验证简历采集插件无法管理流程或发送招聘邮件。
func TestRecruitmentServerRejectsExtensionSession(t *testing.T) {
	t.Setenv("GOODHR_PG_DSN", "")
	t.Setenv("GOODHR_REDIS_ADDR", "")
	t.Setenv("GOODHR_SMTP_HOST", "")
	server := mustNewServer(t)
	if err := server.auth.store.SaveSession("recruitment-plugin-token", Session{Email: "recruitment-plugin@example.com", Kind: extensionSessionKind}, time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/recruitment/workflows", "/api/recruitment/applications", "/api/recruitment/company", "/api/recruitment/mail-send"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer recruitment-plugin-token")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("plugin %s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

// TestRecruitmentServerReadsTenantDefaults 验证真实注册用户能访问本团队初始流程与邮件模板。
func TestRecruitmentServerReadsTenantDefaults(t *testing.T) {
	t.Setenv("GOODHR_PG_DSN", "")
	t.Setenv("GOODHR_REDIS_ADDR", "")
	t.Setenv("GOODHR_SMTP_HOST", "")
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "recruitment-route@example.com")
	for _, path := range []string{"/api/recruitment/workflows", "/api/recruitment/mail-templates", "/api/recruitment/company", "/api/recruitment/placeholders", "/api/recruitment/applications"} {
		status, body := candidateTestRequest(t, routes, token, http.MethodGet, path, nil)
		if status != http.StatusOK {
			t.Errorf("authenticated %s status=%d body=%v", path, status, body)
		}
	}
}
