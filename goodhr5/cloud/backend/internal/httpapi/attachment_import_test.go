// 本文件验证已确认附件的服务器托管保存、付费幂等、旧完整解析复用、重启断点和公开回执隐私，不调用真实 AI。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const attachmentImportAIExample = `{"is_resume":true,"candidate_name":"测试候选人","phone":"13812340000","gender":"女","email":"model@example.com","current_position":"后端开发","birth_ym":"2000","work_experiences":[{"company_name":"后来公司","start_ym":"2023-01","content":"后来的完整职责"},{"company_name":"起步公司","start_ym":"2010","end_ym":"2012-03","content":"最早工作的完整职责"}],"educations":[{"school_name":"后来学校","start_ym":"2020","major_name":"后来的专业"},{"school_name":"测试大学","start_ym":"2006-09","end_ym":"2010-06","major_name":"原专业"}]}`

// attachmentImportCandidateStore 通过原事务实现记录调用次数，并模拟提交回执丢失或独立备注失败。
type attachmentImportCandidateStore struct {
	*MemoryCandidateStore
	saves            atomic.Int32
	noteFailure      bool
	loseFirstReceipt atomic.Bool
	forceSaveError   error
}

// SaveLibraryCandidate 只包装原有存储，测试提交成功但响应丢失时的幂等恢复。
func (s *attachmentImportCandidateStore) SaveLibraryCandidate(scope CandidateLibraryScope, id string, fields map[string]any, version int64, key string) (PositionCandidate, error) {
	s.saves.Add(1)
	if s.forceSaveError != nil {
		return PositionCandidate{}, s.forceSaveError
	}
	item, err := s.MemoryCandidateStore.SaveLibraryCandidate(scope, id, fields, version, key)
	if err == nil && s.loseFirstReceipt.CompareAndSwap(true, false) {
		return PositionCandidate{}, errors.New("模拟事务已提交但连接断开")
	}
	return item, err
}

// SaveCandidateNote 模拟主体保存成功后的独立跟进失败，确保不会回到 AI 阶段。
func (s *attachmentImportCandidateStore) SaveCandidateNote(scope CandidateLibraryScope, item CandidateNote) (CandidateNote, error) {
	if s.noteFailure {
		return CandidateNote{}, errors.New("模拟独立备注失败")
	}
	return s.MemoryCandidateStore.SaveCandidateNote(scope, item)
}

// attachmentImportServiceForTest 使用内存账号、团队和原简历事务构造端到端测试服务。
func attachmentImportServiceForTest(t *testing.T, transport http.RoundTripper) (*AttachmentImportService, *attachmentImportCandidateStore) {
	t.Helper()
	parses := attachmentServiceForTest(t, transport)
	store := &attachmentImportCandidateStore{MemoryCandidateStore: NewMemoryCandidateStore()}
	candidates := NewCandidateService(parses.auth, store, NewMemoryTenantStore(), NewMemoryPositionStore())
	return NewAttachmentImportService(parses, candidates, NewMessageService(parses.auth, NewMemoryMessageStore())), store
}

// attachmentImportInputForTest 提供完整冻结字段，年份精度和空值不由服务端编造。
func attachmentImportInputForTest() attachmentImportRequest {
	parse := attachmentInputForTest("full")
	return attachmentImportRequest{RequestKey: "import-request-0001", CaptureID: parse.CaptureID, Text: parse.Text + "\n出生年份：2000\n邮箱 model@example.com", Source: parse.Source, Operation: "create", Review: attachmentImportReview{Name: "人工核对姓名", Phone: "13812340000", Gender: "female"}, ReviewFields: map[string]string{"first-company": "人工起步公司", "first-work-start": "2010", "first-work-end": "2012-03", "first-school": "人工测试大学", "first-school-start": "2006", "first-school-end": "2010"}, FolderIDs: []string{}, EditedFields: []string{}}
}

// attachmentImportRequestForTest 只通过真实 HTTP 认证入口验证公开合同，支持模拟请求连接关闭。
func attachmentImportRequestForTest(t *testing.T, s *AttachmentImportService, method, path, token string, input any, ctx context.Context) (int, attachmentImportTask, map[string]any) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	if ctx != nil {
		r = r.WithContext(ctx)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var response struct {
		Task attachmentImportTask `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	return w.Code, response.Task, raw
}

// waitAttachmentImportForTest 等待有限时间内的服务器回执，不主动调用模型或重试写入。
func waitAttachmentImportForTest(t *testing.T, s *AttachmentImportService, id string) attachmentParseRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		record, err := s.parses.store.Get(context.Background(), "owner@example.com", id)
		if err == nil && !attachmentTaskActive(record.Task.Status) {
			return record
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("import did not reach a receipt")
	return attachmentParseRecord{}
}

// TestAttachmentImportDetachedAndConcurrent 验证关闭插件连接后继续保存，并发同键也只解析和保存一次。
func TestAttachmentImportDetachedAndConcurrent(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	s, store := attachmentImportServiceForTest(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return attachmentAIResponseForTest(attachmentImportAIExample), nil
	}))
	input := attachmentImportInputForTest()
	ctx, cancel := context.WithCancel(context.Background())
	code, task, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, ctx)
	if code != 202 {
		t.Fatalf("create code=%d task=%+v", code, task)
	}
	cancel()
	var wg sync.WaitGroup
	for index := 0; index < 24; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, same, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
			if code != 200 || same.ID != task.ID {
				t.Errorf("replay=%d %+v", code, same)
			}
		}()
	}
	wg.Wait()
	close(release)
	record := waitAttachmentImportForTest(t, s, task.ID)
	if record.Task.Status != "completed" || calls.Load() != 1 || store.saves.Load() != 1 || record.Import.CandidateID == "" {
		t.Fatalf("record=%+v calls=%d saves=%d", record.Task, calls.Load(), store.saves.Load())
	}
	code, public, raw := attachmentImportRequestForTest(t, s, "GET", attachmentImportPath+"?request_key="+input.RequestKey, "plugin", nil, nil)
	encoded, _ := json.Marshal(raw)
	if code != 200 || public.Status != "completed" || bytes.Contains(encoded, []byte("resume")) || bytes.Contains(encoded, []byte(input.Review.Name)) || bytes.Contains(encoded, []byte("model@example.com")) {
		t.Fatalf("private task leaked: %s", encoded)
	}
	if record.Import.Input.Text != "" || record.Import.Fields != nil || record.Import.Input.Review.Name != "" {
		t.Fatal("terminal record retained private draft")
	}
	item, err := store.GetPositionCandidate(record.Import.Scope.TenantID, record.Import.CandidateID, "", "owner@example.com", true)
	if err != nil || item.RawText != input.Text || item.CandidateName != input.Review.Name || item.LibraryFields["gender"] != "女" {
		t.Fatalf("saved=%+v err=%v", item, err)
	}
	if len(item.WorkExperiences) != 2 || item.WorkExperiences[0].CompanyName != "后来公司" || item.WorkExperiences[1].CompanyName != "人工起步公司" || item.WorkExperiences[1].Content != "最早工作的完整职责" || item.WorkExperiences[1].StartYM != "2010" {
		t.Fatalf("work review lost full rows: %+v", item.WorkExperiences)
	}
	if len(item.Educations) != 2 || item.Educations[1].SchoolName != "人工测试大学" || item.Educations[1].StartYM != "2006" || item.Email != "model@example.com" || item.BirthYM != "" || !strings.Contains(item.BasicInfo, "出生年份：2000") {
		t.Fatalf("school/scalars=%+v", item)
	}
	_, _, raw = attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
	if calls.Load() != 1 || store.saves.Load() != 1 {
		t.Fatalf("terminal replay reran work: %v", raw)
	}
}

// TestAttachmentImportOwnerAndRouteBoundaries 验证同键改意图、跨账号、旧解析取消入口与不受支持会话均不能干扰已确认入库。
func TestAttachmentImportOwnerAndRouteBoundaries(t *testing.T) {
	s, _ := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return attachmentAIResponseForTest(attachmentImportAIExample), nil
	}))
	input := attachmentImportInputForTest()
	code, task, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
	if code != 202 {
		t.Fatal(code)
	}
	_ = waitAttachmentImportForTest(t, s, task.ID)
	changed := input
	changed.Review.Name = "改过的姓名"
	if code, _, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", changed, nil); code != 409 {
		t.Fatalf("changed intent accepted: %d", code)
	}
	for _, path := range []string{attachmentImportPath + "/" + task.ID, attachmentImportPath + "?request_key=" + input.RequestKey} {
		if code, _, _ := attachmentImportRequestForTest(t, s, "GET", path, "other", nil, nil); code != 404 {
			t.Fatalf("cross owner=%d", code)
		}
	}
	for _, request := range []struct{ method, path string }{{"GET", attachmentParsePath + "/" + task.ID}, {"POST", attachmentParsePath + "/" + task.ID + "/cancel"}} {
		if code, _, _ := attachmentRequestForTest(t, s.parses, request.method, request.path, "plugin", nil); code != 404 {
			t.Fatalf("old route accepted import %s=%d", request.path, code)
		}
	}
	if code, _, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "web", input, nil); code != 403 {
		t.Fatal("web session accepted")
	}
}

// TestAttachmentImportReusesCompletedFull 验证保存失败后可复用同账号同快照的旧 full 回执，不依赖当前 AI 配置也不重复收费。
func TestAttachmentImportReusesCompletedFull(t *testing.T) {
	var calls atomic.Int32
	s, store := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return attachmentAIResponseForTest(attachmentImportAIExample), nil
	}))
	input := attachmentImportInputForTest()
	parse := attachmentParseRequest{Phase: "full", RequestKey: "old-full-request-0001", CaptureID: input.CaptureID, Text: input.Text, Source: input.Source}
	code, old, _ := attachmentRequestForTest(t, s.parses, "POST", attachmentParsePath, "plugin", parse)
	if code != 202 {
		t.Fatal(code)
	}
	old = waitAttachmentForTest(t, s.parses, old.ID)
	if old.Status != "completed" {
		t.Fatalf("full=%+v", old)
	}
	input.FullTaskID = old.ID
	s.parses.ai.store = NewMemoryAIConfigStore()
	code, task, raw := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
	if code != 202 {
		t.Fatalf("reuse rejected: %d %v", code, raw)
	}
	record := waitAttachmentImportForTest(t, s, task.ID)
	if record.Task.Status != "completed" || calls.Load() != 1 || store.saves.Load() != 1 {
		t.Fatalf("reuse=%+v calls=%d saves=%d", record.Task, calls.Load(), store.saves.Load())
	}
	for _, change := range []string{"capture", "hash", "source", "owner"} {
		bad := input
		bad.RequestKey = "invalid-reuse-" + change
		bad.Review.Phone = "13912340000"
		token := "plugin"
		switch change {
		case "capture":
			bad.CaptureID = "other-capture"
		case "hash":
			bad.Text += "changed"
		case "source":
			bad.Source.SourceURL = "https://www.zhipin.com/other"
		case "owner":
			token = "other"
		}
		code, _, raw := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, token, bad, nil)
		if code != 409 || raw["error_code"] != "FULL_TASK_MISMATCH" {
			t.Fatalf("%s invalid reuse accepted: %d %v", change, code, raw)
		}
	}
}

// TestAttachmentImportRecoveryDoesNotRebill 验证提交回执未知用同一事务键恢复，模型执行中失联则只报不明而不重发。
func TestAttachmentImportRecoveryDoesNotRebill(t *testing.T) {
	var calls atomic.Int32
	s, store := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return attachmentAIResponseForTest(attachmentImportAIExample), nil
	}))
	store.loseFirstReceipt.Store(true)
	input := attachmentImportInputForTest()
	_, task, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
	record := waitAttachmentImportForTest(t, s, task.ID)
	if record.Task.Status != "uncertain" || record.Import.Stage != "saving" || record.Import.Fields == nil || record.Import.Input.Text == "" {
		t.Fatalf("lost save intent: %+v", record)
	}
	version := record.Version
	record.Version++
	record.LeaseUntil = 0
	if ok, err := s.parses.store.Swap(context.Background(), record, version); err != nil || !ok {
		t.Fatal(err)
	}
	restarted := NewAttachmentImportService(s.parses, s.candidates, s.messages)
	restarted.recoverPending()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, _ = s.parses.store.Get(context.Background(), "owner@example.com", task.ID)
		if record.Task.Status == "completed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if record.Task.Status != "completed" || calls.Load() != 1 || store.saves.Load() != 2 || len(store.profiles) != 1 {
		t.Fatalf("save recovery failed: %+v calls=%d saves=%d profiles=%d", record.Task, calls.Load(), store.saves.Load(), len(store.profiles))
	}
	uncertain := attachmentParseRecord{Owner: "owner@example.com", Version: 1, Task: attachmentParseTask{ID: attachmentImportID("owner@example.com", "crashed-full-0001"), Phase: "import", RequestKey: "crashed-full-0001", Status: "running"}, Import: &attachmentImportState{Input: input, Stage: "parsing"}}
	if _, _, err := s.parses.store.Reserve(context.Background(), uncertain); err != nil {
		t.Fatal(err)
	}
	restarted.recoverPending()
	crashed, err := s.parses.store.Get(context.Background(), uncertain.Owner, uncertain.Task.ID)
	if err != nil || crashed.Task.Status != "uncertain" || crashed.Task.ErrorCode != "AI_RESULT_UNCERTAIN" || !crashed.Task.PossiblyCharged || calls.Load() != 1 {
		t.Fatalf("crashed AI replayed: %+v err=%v calls=%d", crashed.Task, err, calls.Load())
	}
	if crashed.Import.Input.Text != "" {
		t.Fatal("uncertain model retained raw text")
	}
}

// TestAttachmentImportPartialAndConflicts 验证跟进失败只报部分成功，重复电话和陈旧行版本均不触发新 AI 或新建覆盖。
func TestAttachmentImportPartialAndConflicts(t *testing.T) {
	var calls atomic.Int32
	s, store := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return attachmentAIResponseForTest(attachmentImportAIExample), nil
	}))
	store.noteFailure = true
	input := attachmentImportInputForTest()
	input.Note = &attachmentImportNote{Content: "仅供测试的备注"}
	_, task, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
	record := waitAttachmentImportForTest(t, s, task.ID)
	if record.Task.Status != "partial_success" || record.Import.CandidateID == "" || len(record.Import.FollowupErrors) != 1 {
		t.Fatalf("partial=%+v", record)
	}
	_, _, _ = attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
	duplicate := input
	duplicate.RequestKey = "duplicate-new-request-0001"
	duplicate.Note = nil
	code, _, raw := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", duplicate, nil)
	if code != 409 || raw["error_code"] != "CANDIDATE_DUPLICATE" {
		t.Fatalf("duplicate=%d %v", code, raw)
	}
	update := duplicate
	update.Operation = "update"
	update.CandidateID = record.Import.CandidateID
	update.RowVersion = record.Import.RowVersion + 1
	update.RequestKey = "stale-update-request-0001"
	code, _, raw = attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", update, nil)
	if code != 409 || raw["error_code"] != "CANDIDATE_VERSION_CONFLICT" || calls.Load() != 1 || store.saves.Load() != 1 || len(store.profiles) != 1 {
		t.Fatalf("stale update=%d %v calls=%d saves=%d", code, raw, calls.Load(), store.saves.Load())
	}
	update.RowVersion = record.Import.RowVersion
	update.Note = &attachmentImportNote{Content: "不应隐式保存"}
	update.RequestKey = "update-followup-request-0001"
	if code, _, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", update, nil); code != 400 {
		t.Fatalf("update followup accepted: %d", code)
	}
}

// TestAttachmentImportConflictAfterAI 验证解析期间发生的数据库冲突不退回新建，回执明确提示已调用 AI 且重放不收费。
func TestAttachmentImportConflictAfterAI(t *testing.T) {
	for _, conflict := range []error{ErrCandidateDuplicate, ErrCandidateVersion} {
		t.Run(conflict.Error(), func(t *testing.T) {
			var calls atomic.Int32
			s, store := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return attachmentAIResponseForTest(attachmentImportAIExample), nil
			}))
			store.forceSaveError = conflict
			input := attachmentImportInputForTest()
			_, task, _ := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
			record := waitAttachmentImportForTest(t, s, task.ID)
			if record.Task.Status != "failed" || !record.Task.PossiblyCharged || !strings.Contains(record.Task.Message, "已调用 AI") || record.Import.CandidateID != "" || len(store.profiles) != 0 {
				t.Fatalf("business conflict misreported: %+v", record.Task)
			}
			_, _, _ = attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
			if calls.Load() != 1 || store.saves.Load() != 1 {
				t.Fatalf("conflict retried AI=%d saves=%d", calls.Load(), store.saves.Load())
			}
		})
	}
}

// TestAttachmentImportManualClears 验证人工明确清空仅覆盖指定标量和最早履历，不清空 full 提取的未编辑字段。
func TestAttachmentImportManualClears(t *testing.T) {
	input := attachmentImportInputForTest()
	if message := validateAttachmentImportRequest(&input); message != "" {
		t.Fatal(message)
	}
	input.Review.Email = ""
	input.EditedFields = []string{"candidate-email"}
	input.ReviewFields["first-work-end"] = ""
	var parsed map[string]any
	_ = json.Unmarshal([]byte(attachmentImportAIExample), &parsed)
	fields, err := attachmentImportFields(parsed, input)
	if err != nil {
		t.Fatal(err)
	}
	rows := fields["work_experiences"].([]map[string]any)
	if fields["email"] != "" || fields["current_position"] != "后端开发" || rows[1]["end_ym"] != "" || len(rows) != 2 || rows[0]["content"] != "后来的完整职责" {
		t.Fatalf("merge=%v", fields)
	}
}

// TestAttachmentImportUpdateMissingRows 验证完整识别没有经历时不清空旧资料，仅人工明确编辑允许空数组更新。
func TestAttachmentImportUpdateMissingRows(t *testing.T) {
	input := attachmentImportInputForTest()
	input.Operation, input.CandidateID, input.RowVersion = "update", "existing", 1
	for key := range input.ReviewFields {
		input.ReviewFields[key] = ""
	}
	if message := validateAttachmentImportRequest(&input); message != "" {
		t.Fatal(message)
	}
	parsed := map[string]any{"work_experiences": []any{}, "educations": []any{}, "email": "model@example.com"}
	fields, err := attachmentImportFields(parsed, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"work_experiences", "educations"} {
		if _, exists := fields[key]; exists {
			t.Fatalf("missing %s unexpectedly clears existing rows: %v", key, fields)
		}
	}
	input.EditedFields = []string{"first-company"}
	fields, err = attachmentImportFields(parsed, input)
	if err != nil {
		t.Fatal(err)
	}
	if rows, exists := fields["work_experiences"]; !exists || !candidateEmptyValue(rows) {
		t.Fatalf("explicit clear lost: %v", fields)
	}
	if _, exists := fields["educations"]; exists {
		t.Fatal("unrelated school rows cleared")
	}
}

// TestCandidateSalaryPatchShared 验证共享规则支持合法单侧、显式清空和省略，并拒绝与既有另一侧冲突。
func TestCandidateSalaryPatchShared(t *testing.T) {
	min, max := 10.0, 20.0
	current := PositionCandidate{ExpectedSalaryMin: &min, ExpectedSalaryMax: &max}
	for _, test := range []struct {
		name    string
		fields  map[string]any
		invalid bool
	}{
		{"new-min-over-old-max", map[string]any{"expected_salary_min": 25.0}, true},
		{"new-max-under-old-min", map[string]any{"expected_salary_max": 5.0}, true},
		{"legal-min-only", map[string]any{"expected_salary_min": 12.0}, false},
		{"legal-max-only", map[string]any{"expected_salary_max": 15.0}, false},
		{"clear-min", map[string]any{"expected_salary_min": nil, "expected_salary_max": 5.0}, false},
		{"clear-max", map[string]any{"expected_salary_min": 25.0, "expected_salary_max": nil}, false},
		{"unchanged", map[string]any{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateCandidateSalaryPatch(current, test.fields); (err != nil) != test.invalid {
				t.Fatalf("error=%v invalid=%t", err, test.invalid)
			}
		})
	}
	if *current.ExpectedSalaryMin != 10 || *current.ExpectedSalaryMax != 20 {
		t.Fatal("validation mutated current candidate")
	}
}

// TestAttachmentImportUpdateSalaryAndVersion 验证后台更新复用原薪资边界，解析期间的并发修改仍由行版本保护。
func TestAttachmentImportUpdateSalaryAndVersion(t *testing.T) {
	for _, test := range []struct {
		name, key            string
		value                float64
		stale                bool
		wantStatus, wantCode string
	}{
		{"min-over-max", "expected_salary_min", 25, false, "failed", "INVALID_SALARY_RANGE"},
		{"max-under-min", "expected_salary_max", 5, false, "failed", "INVALID_SALARY_RANGE"},
		{"legal-min", "expected_salary_min", 12, false, "completed", ""},
		{"stale-version", "expected_salary_min", 12, true, "failed", "CANDIDATE_VERSION_CONFLICT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			var store *attachmentImportCandidateStore
			var scope CandidateLibraryScope
			var existing PositionCandidate
			s, createdStore := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				if test.stale {
					if _, err := store.MemoryCandidateStore.SaveLibraryCandidate(scope, existing.ID, map[string]any{"personal_description": "并发人工修改保留"}, existing.RowVersion, "concurrent-change"); err != nil {
						t.Error(err)
					}
				}
				var parsed map[string]any
				_ = json.Unmarshal([]byte(attachmentImportAIExample), &parsed)
				parsed[test.key] = test.value
				body, _ := json.Marshal(parsed)
				return attachmentAIResponseForTest(string(body)), nil
			}))
			store = createdStore
			tenant, _ := s.candidates.tenantStore.GetOrCreateTenant("owner@example.com")
			scope = CandidateLibraryScope{TenantID: tenant.ID, UserEmail: "owner@example.com", IsAdmin: true, IsExtension: true}
			var err error
			existing, err = store.MemoryCandidateStore.SaveLibraryCandidate(scope, "", map[string]any{"candidate_name": "已有测试候选人", "phone": "13812340000", "expected_salary_min": 10.0, "expected_salary_max": 20.0}, 0, "existing-candidate")
			if err != nil {
				t.Fatal(err)
			}
			input := attachmentImportInputForTest()
			input.Operation, input.CandidateID, input.RowVersion = "update", existing.ID, existing.RowVersion
			code, task, raw := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
			if code != 202 {
				t.Fatalf("create=%d %v", code, raw)
			}
			record := waitAttachmentImportForTest(t, s, task.ID)
			if record.Task.Status != test.wantStatus || record.Task.ErrorCode != test.wantCode || calls.Load() != 1 {
				t.Fatalf("task=%+v calls=%d", record.Task, calls.Load())
			}
			actual, err := store.GetPositionCandidate(scope.TenantID, existing.ID, "", scope.UserEmail, true)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantStatus == "failed" {
				if !record.Task.PossiblyCharged || !strings.Contains(record.Task.Message, "已调用 AI") || *actual.ExpectedSalaryMin != 10 || *actual.ExpectedSalaryMax != 20 {
					t.Fatalf("failure modified salary or hid AI: %+v current=%+v", record.Task, actual)
				}
				if test.stale && actual.PersonalDescription != "并发人工修改保留" {
					t.Fatal("stale save overwrote concurrent edit")
				}
			} else if *actual.ExpectedSalaryMin != 12 || *actual.ExpectedSalaryMax != 20 {
				t.Fatalf("valid one-sided update=%+v", actual)
			}
		})
	}
}

// TestAttachmentImportSalaryReceiptRecovery 验证提交回执丢失后有合法新薪资修改时，恢复仍读取原幂等结果而不覆盖后来资料。
func TestAttachmentImportSalaryReceiptRecovery(t *testing.T) {
	var calls atomic.Int32
	s, store := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		var parsed map[string]any
		_ = json.Unmarshal([]byte(attachmentImportAIExample), &parsed)
		parsed["expected_salary_min"] = 12.0
		data, _ := json.Marshal(parsed)
		return attachmentAIResponseForTest(string(data)), nil
	}))
	tenant, _ := s.candidates.tenantStore.GetOrCreateTenant("owner@example.com")
	scope := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: "owner@example.com", IsAdmin: true, IsExtension: true}
	existing, err := store.MemoryCandidateStore.SaveLibraryCandidate(scope, "", map[string]any{"candidate_name": "已有测试候选人", "phone": "13812340000", "expected_salary_min": 10.0, "expected_salary_max": 20.0}, 0, "existing-salary")
	if err != nil {
		t.Fatal(err)
	}
	store.loseFirstReceipt.Store(true)
	input := attachmentImportInputForTest()
	input.Operation, input.CandidateID, input.RowVersion = "update", existing.ID, existing.RowVersion
	code, task, raw := attachmentImportRequestForTest(t, s, "POST", attachmentImportPath, "plugin", input, nil)
	if code != 202 {
		t.Fatalf("create=%d %v", code, raw)
	}
	record := waitAttachmentImportForTest(t, s, task.ID)
	if record.Task.Status != "uncertain" || !record.Import.SaveValidated {
		t.Fatalf("missing frozen validation: %+v", record)
	}
	after, _ := store.GetPositionCandidate(scope.TenantID, existing.ID, "", scope.UserEmail, true)
	_, err = store.MemoryCandidateStore.SaveLibraryCandidate(scope, existing.ID, map[string]any{"expected_salary_min": 2.0, "expected_salary_max": 5.0}, after.RowVersion, "later-legal-change")
	if err != nil {
		t.Fatal(err)
	}
	version := record.Version
	record.Version++
	record.LeaseUntil = 0
	if ok, err := s.parses.store.Swap(context.Background(), record, version); err != nil || !ok {
		t.Fatal(err)
	}
	s.reconcile(record)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, _ = s.parses.store.Get(context.Background(), record.Owner, task.ID)
		if record.Task.Status == "completed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	actual, err := store.GetPositionCandidate(scope.TenantID, existing.ID, "", scope.UserEmail, true)
	if err != nil || record.Task.Status != "completed" || *actual.ExpectedSalaryMin != 2 || *actual.ExpectedSalaryMax != 5 || calls.Load() != 1 || store.saves.Load() != 2 {
		t.Fatalf("idempotency contradicted later data: task=%+v actual=%+v err=%v AI=%d saves=%d", record.Task, actual, err, calls.Load(), store.saves.Load())
	}
}

// TestAttachmentImportQueuedRecovery 验证已持久确认但尚未发出模型的任务可被两个重启实例竞争接管一次。
func TestAttachmentImportQueuedRecovery(t *testing.T) {
	var calls atomic.Int32
	s, store := attachmentImportServiceForTest(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return attachmentAIResponseForTest(attachmentImportAIExample), nil
	}))
	input := attachmentImportInputForTest()
	if message := validateAttachmentImportRequest(&input); message != "" {
		t.Fatal(message)
	}
	tenant, _ := s.candidates.tenantStore.GetOrCreateTenant("owner@example.com")
	record := attachmentParseRecord{Owner: "owner@example.com", Version: 1, Task: attachmentParseTask{ID: attachmentImportID("owner@example.com", input.RequestKey), Phase: "import", Status: "queued", RequestKey: input.RequestKey, CaptureID: input.CaptureID, TextHash: input.TextHash}, Import: &attachmentImportState{Input: input, Scope: CandidateLibraryScope{TenantID: tenant.ID, UserEmail: "owner@example.com", IsExtension: true}, Stage: "queued"}}
	if _, _, err := s.parses.store.Reserve(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	other := NewAttachmentImportService(s.parses, s.candidates, s.messages)
	var wg sync.WaitGroup
	for _, service := range []*AttachmentImportService{s, other} {
		wg.Add(1)
		go func(service *AttachmentImportService) { defer wg.Done(); service.recoverPending() }(service)
	}
	wg.Wait()
	result := waitAttachmentImportForTest(t, s, record.Task.ID)
	if result.Task.Status != "completed" || calls.Load() != 1 || store.saves.Load() != 1 {
		t.Fatalf("queued recovery=%+v calls=%d saves=%d", result.Task, calls.Load(), store.saves.Load())
	}
}

// TestRedisAttachmentImportRecoveryIndex 使用显式本机测试 Redis 验证确认意图和断点可被新实例读取，终态退出恢复索引。
func TestRedisAttachmentImportRecoveryIndex(t *testing.T) {
	addr := os.Getenv("GOODHR_EXTENSION_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("GOODHR_EXTENSION_REDIS_TEST_ADDR is not set")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		t.Fatal("requires explicit loopback test Redis")
	}
	auth := NewRedisAuthStore(addr, "", 0)
	t.Cleanup(func() { _ = auth.client.Close() })
	first := newAttachmentParseStore(auth).(*redisAttachmentParseStore)
	second := &redisAttachmentParseStore{client: auth.client}
	owner := "import-index-" + fmt.Sprint(time.Now().UnixNano()) + "@example.com"
	record := attachmentParseRecord{Owner: owner, Version: 1, LeaseUntil: time.Now().Add(time.Minute).UnixMilli(), Task: attachmentParseTask{ID: attachmentDigest(owner), Phase: "import", Status: "queued"}, Import: &attachmentImportState{Stage: "queued", Input: attachmentImportInputForTest()}}
	key := attachmentRecordKey(owner, record.Task.ID)
	t.Cleanup(func() {
		_ = auth.client.Del(context.Background(), key, attachmentActiveKey(owner)).Err()
		_ = auth.client.ZRem(context.Background(), attachmentImportPendingKey, key).Err()
	})
	if _, created, err := first.Reserve(context.Background(), record); err != nil || !created {
		t.Fatalf("reserve=%t err=%v", created, err)
	}
	items, err := second.PendingImports(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.Task.ID == record.Task.ID {
			found = item.Import.Input.Text == record.Import.Input.Text
		}
	}
	if !found {
		t.Fatal("confirmed raw intent missing from restart index")
	}
	record.Version++
	record.Task.Status = "completed"
	record.Import.Stage = "done"
	record.Import.Input.Text = ""
	if ok, err := first.Swap(context.Background(), record, 1); err != nil || !ok {
		t.Fatal(err)
	}
	items, err = second.PendingImports(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Task.ID == record.Task.ID {
			t.Fatal("terminal task still scheduled")
		}
	}
	if exists, err := auth.client.Exists(context.Background(), key).Result(); err != nil || exists != 1 {
		t.Fatal("terminal idempotency receipt disappeared")
	}
}
