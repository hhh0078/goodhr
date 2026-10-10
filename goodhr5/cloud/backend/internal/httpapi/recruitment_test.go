// 本文件验证人工启用招聘、团队授权、模板快照、面评审核门槛、并发修订和邀请失效。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recruitmentFixture 创建包含招聘负责人、两位面试官、审核人和抄送人的隔离团队。
func recruitmentFixture(t *testing.T) (*RecruitmentService, CandidateLibraryScope, *MemoryCandidateStore) {
	t.Helper()
	tenants := NewMemoryTenantStore()
	tenant, err := tenants.GetOrCreateTenant("owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"one@example.com", "two@example.com", "cc@example.com", "unrelated@example.com"} {
		tenants.members[tenant.ID] = append(tenants.members[tenant.ID], TenantMember{Email: email, Role: "member", Status: "active"})
	}
	scope := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: "owner@example.com", IsAdmin: true}
	candidates := NewMemoryCandidateStore()
	candidates.profiles["candidate"] = PositionCandidate{ID: "candidate", TenantID: tenant.ID, UserEmail: scope.UserEmail, CandidateName: "测试同学", Email: "candidate@example.com", Phone: "13800138000"}
	positions := NewMemoryPositionStore()
	positions.positions["position"] = Position{ID: "position", UserEmail: scope.UserEmail, Name: "测试岗位"}
	_, err = candidates.SaveCandidateRecommendation(scope, "candidate", positions.positions["position"], "interview", "旧猎头面试备注", "legacy-recommendation")
	if err != nil {
		t.Fatal(err)
	}
	return NewRecruitmentService(nil, NewMemoryRecruitmentStore(), tenants, candidates, positions), scope, candidates
}

// recruitmentTestWorkflow 创建带多个参与者的测试流程。
func recruitmentTestWorkflow(t *testing.T, s *RecruitmentService, scope CandidateLibraryScope, mode string) RecruitmentWorkflow {
	t.Helper()
	workflow := defaultRecruitmentWorkflow(scope.TenantID)
	workflow.Nodes = []RecruitmentNode{{ID: "review", Name: "筛选", Type: "review", OwnerEmail: scope.UserEmail, ApprovalMode: "all", CCEmails: []string{"cc@example.com"}}, {ID: "interview", Name: "复试", Type: "interview", OwnerEmail: scope.UserEmail, InterviewerEmails: []string{"one@example.com", "two@example.com"}, ApproverEmails: []string{"one@example.com", "two@example.com"}, CCEmails: []string{"cc@example.com"}, ApprovalMode: mode, RequiresFeedback: true}, {ID: "onboarding", Name: "入职确认", Type: "onboarding", OwnerEmail: scope.UserEmail, ApprovalMode: "all"}}
	if err := s.validateWorkflow(scope, &workflow); err != nil {
		t.Fatal(err)
	}
	saved, err := s.store.SaveWorkflow(scope.TenantID, workflow, 0)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// recruitmentTestAct 提交一个动作并返回新修订，减少测试中的重复样板。
func recruitmentTestAct(t *testing.T, s *RecruitmentService, scope CandidateLibraryScope, app RecruitmentApplication, input RecruitmentAction) RecruitmentApplication {
	t.Helper()
	input.Revision = app.Revision
	saved, err := s.Act(scope, app.ID, input)
	if err != nil {
		t.Fatalf("action=%s err=%v", input.Action, err)
	}
	return saved
}

// TestRecruitmentManualStartPreservesHistory 验证历史面试状态不自动启动，显式启动保存模板快照和原记录。
func TestRecruitmentManualStartPreservesHistory(t *testing.T) {
	s, scope, candidates := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, s, scope, "all")
	before, _ := candidates.ListCandidateRecommendations(scope, "candidate")
	items, _ := s.store.ListApplications(scope.TenantID)
	if len(items) != 0 {
		t.Fatal("historical interview unexpectedly created an HR process")
	}
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if app.RecommendationID != before[0].ID || app.Stage != "contacting" || app.CurrentNode != 0 {
		t.Fatalf("unexpected application %+v", app)
	}
	if _, err = s.StartApplication(scope, "candidate", "position", workflow.ID, ""); !errors.Is(err, ErrRecruitmentDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	workflow.Nodes[0].Name = "修改后的模板"
	workflow, err = s.store.SaveWorkflow(scope.TenantID, workflow, workflow.Revision)
	if err != nil {
		t.Fatal(err)
	}
	read, _ := s.store.GetApplication(scope.TenantID, app.ID)
	if read.Nodes[0].Name == workflow.Nodes[0].Name || read.WorkflowVersion != 1 {
		t.Fatal("template edit changed ongoing snapshot")
	}
	after, _ := candidates.ListCandidateRecommendations(scope, "candidate")
	rawBefore, _ := json.Marshal(before)
	rawAfter, _ := json.Marshal(after)
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("new process overwrote headhunter history")
	}
	if _, err = s.StartApplication(scope, builtinCandidateID, "position", workflow.ID, ""); err == nil {
		t.Fatal("builtin candidate accepted")
	}
}

// TestRecruitmentMailUsesCurrentCandidateEmail 验证启动后补充或清空邮箱只影响新邮件，流程启动快照和历史不改写。
func TestRecruitmentMailUsesCurrentCandidateEmail(t *testing.T) {
	s, scope, candidates := recruitmentFixture(t)
	candidate := candidates.profiles["candidate"]
	candidate.Email = ""
	candidates.profiles["candidate"] = candidate
	workflow := recruitmentTestWorkflow(t, s, scope, "all")
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	candidate.Email = "new-address@example.com"
	candidate.CandidateName = "补充后的姓名"
	candidates.profiles["candidate"] = candidate
	ctx, err := s.MailContext(scope, app.ID, "")
	if err != nil || ctx.CandidateEmail != candidate.Email || ctx.CandidateName != candidate.CandidateName {
		t.Fatalf("context=%+v err=%v", ctx, err)
	}
	read, _ := s.store.GetApplication(scope.TenantID, app.ID)
	if read.CandidateEmail != "" {
		t.Fatal("mail context changed original snapshot")
	}
	candidate.Email = ""
	candidates.profiles["candidate"] = candidate
	ctx, err = s.MailContext(scope, app.ID, "")
	if err != nil || ctx.CandidateEmail != "" {
		t.Fatalf("cleared email ignored %+v err=%v", ctx, err)
	}
	delete(candidates.profiles, "candidate")
	if _, err = s.MailContext(scope, app.ID, ""); err == nil || !strings.Contains(err.Error(), "简历已删除") {
		t.Fatalf("deleted candidate can send: %v", err)
	}
}

// TestRecruitmentDefaultIdempotencyAndWorkflowRevision 验证默认初始化并发幂等、模板修订冲突和默认删除限制。
func TestRecruitmentDefaultIdempotencyAndWorkflowRevision(t *testing.T) {
	store := NewMemoryRecruitmentStore()
	var wait sync.WaitGroup
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := store.EnsureDefault("team", defaultRecruitmentWorkflow("team")); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	items, _ := store.ListWorkflows("team")
	if len(items) != 1 {
		t.Fatalf("defaults=%d", len(items))
	}
	first := items[0]
	first.Name = "修改"
	saved, err := store.SaveWorkflow("team", first, first.Revision)
	if err != nil || saved.Version != 2 {
		t.Fatalf("save %+v %v", saved, err)
	}
	if _, err = store.SaveWorkflow("team", first, first.Revision); !errors.Is(err, ErrRecruitmentVersion) {
		t.Fatalf("stale edit=%v", err)
	}
	if err = store.DeleteWorkflow("team", saved.ID, saved.Revision); !errors.Is(err, ErrRecruitmentInvalid) {
		t.Fatalf("default delete=%v", err)
	}
}

// TestRecruitmentMemberBoundaryAndCCReadOnly 验证任务访问仅限指定团队成员，抄送无法推进、发邮件或查看其他简历。
func TestRecruitmentMemberBoundaryAndCCReadOnly(t *testing.T) {
	s, scope, candidates := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, s, scope, "all")
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	cc := CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "cc@example.com"}
	visible, err := s.GetApplication(cc, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if visible.Permissions != (RecruitmentPermissions{}) {
		t.Fatalf("CC permissions %+v", visible.Permissions)
	}
	if !recruitmentPendingFor(cc, visible) {
		t.Fatal("CC notification is missing")
	}
	if _, err = s.Act(cc, app.ID, RecruitmentAction{Revision: app.Revision, Action: "advance"}); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("CC advance=%v", err)
	}
	if _, err = s.MailContext(cc, app.ID, ""); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("CC mail=%v", err)
	}
	app = recruitmentTestAct(t, s, cc, app, RecruitmentAction{Action: "read_notifications"})
	if recruitmentPendingFor(cc, app) {
		t.Fatal("acknowledged CC remains pending")
	}
	if _, err = candidates.GetPositionCandidate(scope.TenantID, "candidate", "", cc.UserEmail, false); err == nil {
		t.Fatal("CC gained unrestricted candidate access")
	}
	if _, err = s.GetApplication(CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "unrelated@example.com"}, app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unrelated member=%v", err)
	}
	if _, err = s.GetApplication(CandidateLibraryScope{TenantID: "other", UserEmail: scope.UserEmail, IsAdmin: true}, app.ID); err == nil {
		t.Fatal("cross tenant access")
	}
	tenants := s.tenants.(*MemoryTenantStore)
	for i, member := range tenants.members[scope.TenantID] {
		if member.Email == cc.UserEmail {
			tenants.members[scope.TenantID] = append(tenants.members[scope.TenantID][:i], tenants.members[scope.TenantID][i+1:]...)
			break
		}
	}
	if _, err = s.GetApplication(cc, app.ID); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("departed member access=%v", err)
	}
}

// TestRecruitmentMultiInterviewFeedbackAndApproval 验证两场面试、所有面评和全员审核齐备后才能推进。
func TestRecruitmentMultiInterviewFeedbackAndApproval(t *testing.T) {
	s, scope, _ := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, s, scope, "all")
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "advance"})
	for i := 0; i < 2; i++ {
		app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "schedule", StartsAt: time.Now().Add(time.Hour), DurationMinutes: 30, Mode: "video", MeetingURL: "https://meeting.example.com/one"})
	}
	if len(app.Interviews) != 2 {
		t.Fatal("multi-session scheduling failed")
	}
	if _, err = s.Act(scope, app.ID, RecruitmentAction{Revision: app.Revision, Action: "advance"}); err == nil {
		t.Fatal("advanced before interviews")
	}
	app, err = s.store.MutateApplication(scope.TenantID, app.ID, app.Revision, func(item *RecruitmentApplication) error {
		for i := range item.Interviews {
			item.Interviews[i].StartsAt = time.Now().Add(-time.Hour)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, interview := range app.Interviews {
		app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "complete_interview", InterviewID: interview.ID})
		for _, email := range []string{"one@example.com", "two@example.com"} {
			member := CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: email}
			app = recruitmentTestAct(t, s, member, app, RecruitmentAction{Action: "feedback", InterviewID: interview.ID, Conclusion: "pass", Content: "独立面评"})
		}
	}
	one := CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "one@example.com"}
	two := CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "two@example.com"}
	app = recruitmentTestAct(t, s, one, app, RecruitmentAction{Action: "approve", Decision: "approved"})
	if _, err = s.Act(scope, app.ID, RecruitmentAction{Revision: app.Revision, Action: "advance"}); err == nil {
		t.Fatal("all approval accepted one vote")
	}
	app = recruitmentTestAct(t, s, two, app, RecruitmentAction{Action: "approve", Decision: "approved"})
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "advance"})
	if app.CurrentNode != 2 || app.Stage != "offer" {
		t.Fatalf("advance %+v", app)
	}
	if _, err = s.Act(scope, app.ID, RecruitmentAction{Revision: app.Revision, Action: "advance"}); err == nil {
		t.Fatal("hired without entry date")
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "set_entry_date", EntryDate: time.Now().Add(-24 * time.Hour).Format("2006-01-02")})
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "advance"})
	if app.Status != "completed" || app.Stage != "hired" {
		t.Fatalf("not completed %+v", app)
	}
}

// TestRecruitmentAnyApprovalAndReturnRound 验证任一人审核和退回重新处理，旧审批与面评不会满足新轮次。
func TestRecruitmentAnyApprovalAndReturnRound(t *testing.T) {
	s, scope, _ := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, s, scope, "any")
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "advance"})
	app.Nodes[1].RequiresFeedback = false
	node := app.Nodes[1]
	node.Approvals = []RecruitmentDecision{{AuthorEmail: "one@example.com", Decision: "approved"}, {AuthorEmail: "two@example.com", Decision: "rejected"}}
	if !recruitmentApprovalReady(node) {
		t.Fatal("any approval blocked by another rejection")
	}
	node.ApprovalMode = "all"
	if recruitmentApprovalReady(node) {
		t.Fatal("all approval ignored rejection")
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "schedule", StartsAt: time.Now().Add(time.Hour), DurationMinutes: 30, Mode: "phone"})
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "return", TargetNodeIndex: 0, Note: "补充筛选材料"})
	if app.Nodes[1].Attempt != 2 || len(app.Nodes[1].Approvals) != 0 || app.Interviews[0].Status != "cancelled" {
		t.Fatalf("return failed %+v", app)
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "advance"})
	if err = recruitmentAdvanceReady(app, app.Nodes[1]); err == nil {
		t.Fatal("old interview satisfied new round")
	}
}

// TestRecruitmentConcurrentActionsAndImmutableFailure 验证同一修订的并发操作只有一次成功，错误动作不修改状态。
func TestRecruitmentConcurrentActionsAndImmutableFailure(t *testing.T) {
	s, scope, _ := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, s, scope, "all")
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	conflict := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Act(scope, app.ID, RecruitmentAction{Revision: app.Revision, Action: "advance"})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if errors.Is(err, ErrRecruitmentVersion) {
				conflict++
			} else {
				t.Errorf("unexpected %v", err)
			}
		}()
	}
	wg.Wait()
	if success != 1 || conflict != 11 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	read, _ := s.store.GetApplication(scope.TenantID, app.ID)
	revision := read.Revision
	if _, err = s.Act(scope, app.ID, RecruitmentAction{Revision: revision, Action: "schedule", StartsAt: time.Now().Add(time.Hour), DurationMinutes: 30, Mode: "phone", InterviewerEmails: []string{"not-a-member@example.com"}}); err == nil {
		t.Fatal("external interviewer accepted")
	}
	after, _ := s.store.GetApplication(scope.TenantID, app.ID)
	if after.Revision != revision || len(after.Interviews) != 0 {
		t.Fatal("failed action mutated stored application")
	}
}

// TestRecruitmentInvitationScopeAndRescheduleInvalidation 验证候选人链接只公开单场邀请，取消改期使旧链接失效。
func TestRecruitmentInvitationScopeAndRescheduleInvalidation(t *testing.T) {
	s, scope, _ := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, s, scope, "all")
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "advance"})
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "schedule", StartsAt: time.Now().Add(time.Hour), DurationMinutes: 30, Mode: "phone"})
	interview := app.Interviews[0]
	token, err := s.InvitationToken(scope, app.ID, interview.ID, interview.Revision)
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRecorder()
	s.Invitation(get, httptest.NewRequest(http.MethodGet, "/api/public/recruitment/invitations/"+token, nil))
	if get.Code != 200 {
		t.Fatal(get.Body.String())
	}
	if strings.Contains(get.Body.String(), "approvals") || strings.Contains(get.Body.String(), "one@example.com") || strings.Contains(get.Body.String(), "candidate@example.com") {
		t.Fatal("invitation leaked internal details")
	}
	post := httptest.NewRecorder()
	s.Invitation(post, httptest.NewRequest(http.MethodPost, "/api/public/recruitment/invitations/"+token, strings.NewReader(`{"action":"confirm"}`)))
	if post.Code != 200 {
		t.Fatal(post.Body.String())
	}
	app, _ = s.store.GetApplication(scope.TenantID, app.ID)
	if app.Interviews[0].Confirmation != "confirmed" {
		t.Fatal("candidate response not persisted")
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "reschedule", InterviewID: interview.ID, StartsAt: time.Now().Add(2 * time.Hour), DurationMinutes: 45, Mode: "phone"})
	if _, _, _, err = s.recruitmentInvitationContext(token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token valid after reschedule=%v", err)
	}
	newToken, err := s.InvitationToken(scope, app.ID, interview.ID, app.Interviews[0].Revision)
	if err != nil {
		t.Fatal(err)
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "cancel_interview", InterviewID: interview.ID})
	if _, _, _, err = s.recruitmentInvitationContext(newToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled token=%v", err)
	}
}

// TestRecruitmentReassignDepartedInterviewer 验证离职后可以受控转交场次，抄送不能转交，原面评历史保留且旧邀请失效。
func TestRecruitmentReassignDepartedInterviewer(t *testing.T) {
	s, scope, _ := recruitmentFixture(t)
	workflow := recruitmentTestWorkflow(t, s, scope, "all")
	app, err := s.StartApplication(scope, "candidate", "position", workflow.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "advance"})
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "schedule", StartsAt: time.Now().Add(time.Hour), DurationMinutes: 30, Mode: "phone"})
	interviewID := app.Interviews[0].ID
	token, err := s.InvitationToken(scope, app.ID, interviewID, 1)
	if err != nil {
		t.Fatal(err)
	}
	cc := CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "cc@example.com"}
	if _, err = s.Act(cc, app.ID, RecruitmentAction{Revision: app.Revision, Action: "assign_interviewers", InterviewID: interviewID, InterviewerEmails: []string{"two@example.com"}}); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("CC transferred interviewer=%v", err)
	}
	tenants := s.tenants.(*MemoryTenantStore)
	for i, member := range tenants.members[scope.TenantID] {
		if member.Email == "one@example.com" {
			tenants.members[scope.TenantID] = append(tenants.members[scope.TenantID][:i], tenants.members[scope.TenantID][i+1:]...)
			break
		}
	}
	if _, err = s.Act(scope, app.ID, RecruitmentAction{Revision: app.Revision, Action: "assign_interviewers", InterviewID: interviewID, InterviewerEmails: []string{"one@example.com"}}); err == nil {
		t.Fatal("departed interviewer can be reassigned")
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "assign_interviewers", InterviewID: interviewID, InterviewerEmails: []string{"two@example.com"}})
	if _, _, _, err = s.recruitmentInvitationContext(token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old invitation survived transfer %v", err)
	}
	app, err = s.store.MutateApplication(scope.TenantID, app.ID, app.Revision, func(item *RecruitmentApplication) error {
		item.Interviews[0].StartsAt = time.Now().Add(-time.Hour)
		item.Interviews[0].Status = "completed"
		item.Interviews[0].Feedback = []RecruitmentFeedback{{AuthorEmail: "one@example.com", Conclusion: "pass", Content: "保留的历史评价", CreatedAt: time.Now().Add(-time.Minute)}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	app = recruitmentTestAct(t, s, scope, app, RecruitmentAction{Action: "assign_interviewers", InterviewID: interviewID, InterviewerEmails: []string{"unrelated@example.com"}})
	replacement := CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "unrelated@example.com"}
	app = recruitmentTestAct(t, s, replacement, app, RecruitmentAction{Action: "feedback", InterviewID: interviewID, Conclusion: "pass", Content: "转交后的独立评价"})
	if len(app.Interviews[0].Feedback) != 2 || app.Interviews[0].Feedback[0].Content != "保留的历史评价" {
		t.Fatal("historical feedback lost on transfer")
	}
	if _, err = s.GetApplication(CandidateLibraryScope{TenantID: scope.TenantID, UserEmail: "one@example.com"}, app.ID); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("departed interviewer still has access=%v", err)
	}
}
