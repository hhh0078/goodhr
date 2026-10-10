// 本文件只使用显式回环测试数据库的独立schema验证邮件模板初始化和跨进程幂等发送账本。
package httpapi

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestRecruitmentMailPostgresPersistence 验证真实迁移、团队隔离、一次初始化及并发独占发送。
func TestRecruitmentMailPostgresPersistence(t *testing.T) {
	db := candidatePostgresTestDB(t)
	for _, name := range []string{"0090_recruitment_workflows.sql", "0091_recruitment_mail.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	var tenant, otherTenant string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&otherTenant); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresRecruitmentMailStore(db)
	var group sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			templates, err := store.ListTemplates(tenant)
			if err == nil && len(templates) != 7 {
				err = errors.New("预置模板重复或未初始化完整")
			}
			failures <- err
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	company := RecruitmentCompany{CompanyName: "独立测试公司", ContactName: "招聘测试", ContactEmail: "test-hr@example.com"}
	if err := store.SaveCompany(tenant, company); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewPostgresRecruitmentMailStore(db).GetCompany(tenant)
	if err != nil || loaded != company {
		t.Fatalf("company=%+v err=%v", loaded, err)
	}
	templates, err := store.ListTemplates(tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetTemplate(otherTenant, templates[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-team template visible")
	}
	for _, item := range templates {
		if err = store.DeleteTemplate(tenant, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	empty, err := store.ListTemplates(tenant)
	if err != nil || len(empty) != 0 {
		t.Fatalf("deleted defaults came back=%v err=%v", empty, err)
	}
	input := RecruitmentMailDelivery{TenantID: tenant, ActorEmail: "owner@example.com", ApplicationID: recruitmentID(), InterviewID: recruitmentID(), Recipient: "candidate@example.com", ReplyTo: company.ContactEmail, Subject: "[独立测试公司] 面试通知", Body: "你好", HTML: "<div>你好</div>", IdempotencyKey: "parallel-send", Fingerprint: "same-request", ApplicationRevision: 7, InterviewRevision: 2}
	type claimResult struct {
		item  RecruitmentMailDelivery
		owned bool
		err   error
	}
	results := make(chan claimResult, 12)
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			item, owned, err := NewPostgresRecruitmentMailStore(db).ClaimDelivery(input)
			results <- claimResult{item, owned, err}
		}()
	}
	group.Wait()
	close(results)
	claimed := 0
	var winner RecruitmentMailDelivery
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.owned {
			claimed++
			winner = result.item
		}
	}
	if claimed != 1 || winner.Status != "sending" || winner.Prepared {
		t.Fatalf("claims=%d winner=%+v", claimed, winner)
	}
	winner.Body = "正文与受限确认链接快照"
	winner.HTML = "<div>正文与受限确认链接快照</div>"
	prepared, err := store.PrepareDelivery(winner)
	if err != nil || !prepared.Prepared {
		t.Fatalf("prepare=%+v err=%v", prepared, err)
	}
	if _, err = store.PrepareDelivery(winner); !errors.Is(err, ErrRecruitmentMailConflict) {
		t.Fatalf("second prepare err=%v", err)
	}
	uncertain, err := store.FinishDelivery(tenant, winner.ID, "uncertain", "缺少最终确认")
	if err != nil || uncertain.Status != "uncertain" || uncertain.FinishedAt == nil {
		t.Fatalf("uncertain=%+v err=%v", uncertain, err)
	}
	reloaded, found, err := NewPostgresRecruitmentMailStore(db).FindDelivery(tenant, input.ActorEmail, input.IdempotencyKey)
	if err != nil || !found || reloaded.Status != "uncertain" || reloaded.Body != winner.Body || !reloaded.Prepared {
		t.Fatalf("persistent snapshot=%+v found=%v err=%v", reloaded, found, err)
	}
	replay, owned, err := store.ClaimDelivery(input)
	if err != nil || owned || replay.ID != winner.ID || replay.Status != "uncertain" {
		t.Fatalf("uncertain reclaimed=%+v owned=%v err=%v", replay, owned, err)
	}
	input.Fingerprint = "changed-request"
	if _, _, err = store.ClaimDelivery(input); !errors.Is(err, ErrRecruitmentMailConflict) {
		t.Fatalf("fingerprint misuse=%v", err)
	}
	history, err := store.ListDeliveries(otherTenant, input.ApplicationID)
	if err != nil || len(history) != 0 {
		t.Fatalf("cross-team history=%v err=%v", history, err)
	}
}
