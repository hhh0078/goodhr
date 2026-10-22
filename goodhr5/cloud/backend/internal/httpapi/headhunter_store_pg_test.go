// 本文件仅在显式回环地址_test数据库的独立schema中验证猎头迁移、关联、权限和并发，不连接生产。
package httpapi

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// headhunterPostgresFixture 复用本机测试数据库护栏，为完整业务JD和平台绑定准备独立团队。
func headhunterPostgresFixture(t *testing.T) (*sql.DB, *PostgresHeadhunterStore, CandidateLibraryScope, string) {
	t.Helper()
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'; ALTER TABLE positions ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id);`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0090_recruitment_workflows.sql", "0099_headhunter_customers_jobs.sql", "0099_headhunter_customers_jobs.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	var tenantID, userID, positionID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id) VALUES('hunter@example.com',$1) RETURNING id::text`, tenantID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(name,user_id) VALUES('平台招聘岗位',$1) RETURNING id::text`, userID).Scan(&positionID); err != nil {
		t.Fatal(err)
	}
	return db, NewPostgresHeadhunterStore(db), CandidateLibraryScope{TenantID: tenantID, UserEmail: "hunter@example.com", IsAdmin: true}, positionID
}

// TestHeadhunterPostgresLifecycle 验证真实持久化、完整JD查询、中文备注和关联安全删除。
func TestHeadhunterPostgresLifecycle(t *testing.T) {
	db, store, scope, positionID := headhunterPostgresFixture(t)
	customer, err := store.SaveCustomer(scope, HeadhunterCustomer{Name: "真实SQL甲方", OwnerEmail: scope.UserEmail, Contacts: []HeadhunterContact{{Name: "张经理", Phone: "13800138000"}}, Contracts: []HeadhunterContract{{Name: "协议", URL: "https://example.com/agreement.pdf"}}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.SaveJob(scope, HeadhunterJob{Name: "Go工程师", CustomerID: customer.ID, Description: "建设检索服务", Requirements: "熟悉Go，检索经验优先", OwnerEmail: scope.UserEmail, SalaryMin: 35.5, SalaryMax: 65, PositionIDs: []string{positionID}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := store.JobByID(scope, job.ID)
	if err != nil || read.CustomerName != customer.Name || read.SalaryMin != 35.5 || len(read.PositionIDs) != 1 {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	page, err := store.ListJobs(scope, HeadhunterQuery{Keyword: "检索", CustomerID: customer.ID, PageSize: 1})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	customers, err := store.ListCustomers(scope, HeadhunterQuery{})
	if err != nil || customers.Total != 1 || customers.Items[0].Jobs != 1 {
		t.Fatalf("customers=%+v err=%v", customers, err)
	}
	if err = store.DeleteCustomer(scope, customer.ID, customer.Revision); !errors.Is(err, ErrHeadhunterLinked) {
		t.Fatalf("linked customer delete=%v", err)
	}
	if err = store.DeleteJob(scope, job.ID, job.Revision); !errors.Is(err, ErrHeadhunterLinked) {
		t.Fatalf("bound job delete=%v", err)
	}
	job.PositionIDs = []string{}
	job, err = store.SaveJob(scope, job)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteJob(scope, job.ID, job.Revision); err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteCustomer(scope, customer.ID, customer.Revision); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM positions WHERE id=$1`, positionID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("platform count=%d err=%v", count, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN ('headhunter_customers','headhunter_jobs','headhunter_position_bindings') AND a.attnum>0 AND NOT a.attisdropped AND col_description(c.oid,a.attnum) IS NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("missing comments=%d err=%v", count, err)
	}
}

// TestHeadhunterPostgresIsolationAndHistory 验证跨团队绑定拒绝、离职负责人拒绝和解绑后的推荐历史删除保护。
func TestHeadhunterPostgresIsolationAndHistory(t *testing.T) {
	db, store, scope, positionID := headhunterPostgresFixture(t)
	customer, err := store.SaveCustomer(scope, HeadhunterCustomer{Name: "历史甲方", OwnerEmail: scope.UserEmail})
	if err != nil {
		t.Fatal(err)
	}
	var externalTenant, externalUser, externalPosition string
	if err = db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&externalTenant); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO users(email,tenant_id) VALUES('other@example.com',$1) RETURNING id::text`, externalTenant).Scan(&externalUser); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO positions(name,user_id) VALUES('外部岗位',$1) RETURNING id::text`, externalUser).Scan(&externalPosition); err != nil {
		t.Fatal(err)
	}
	job := HeadhunterJob{Name: "甲方岗位", CustomerID: customer.ID, OwnerEmail: scope.UserEmail, PositionIDs: []string{externalPosition}}
	if _, err = store.SaveJob(scope, job); !errors.Is(err, ErrHeadhunterInvalid) {
		t.Fatalf("cross tenant binding=%v", err)
	}
	cross := scope
	cross.TenantID = externalTenant
	if _, err = store.CustomerByID(cross, customer.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross read=%v", err)
	}
	job.PositionIDs = []string{positionID}
	job, err = store.SaveJob(scope, job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE users SET status='disabled' WHERE email=$1`, scope.UserEmail); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveJob(scope, job); !errors.Is(err, ErrHeadhunterInvalid) {
		t.Fatalf("inactive owner=%v", err)
	}
	if _, err = db.Exec(`UPDATE users SET status='active' WHERE email=$1`, scope.UserEmail); err != nil {
		t.Fatal(err)
	}
	job.PositionIDs = []string{}
	job, err = store.SaveJob(scope, job)
	if err != nil {
		t.Fatal(err)
	}
	var candidateID string
	if err = db.QueryRow(`INSERT INTO candidate_profiles(tenant_id,candidate_name) VALUES($1,'推荐候选人') RETURNING id::text`, scope.TenantID).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO candidate_recommendations(tenant_id,candidate_id,position_id,position_key,position_name,stage,author_email,author_name) VALUES($1,$2,$3::uuid,$3::text,'平台招聘岗位','recommended',$4,'测试顾问')`, scope.TenantID, candidateID, positionID, scope.UserEmail); err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteJob(scope, job.ID, job.Revision); !errors.Is(err, ErrHeadhunterLinked) {
		t.Fatalf("history delete=%v", err)
	}
}

// TestHeadhunterPostgresConcurrentRevision 验证真实行锁保证同一修订仅一次保存成功。
func TestHeadhunterPostgresConcurrentRevision(t *testing.T) {
	_, store, scope, _ := headhunterPostgresFixture(t)
	customer, err := store.SaveCustomer(scope, HeadhunterCustomer{Name: "并发客户", OwnerEmail: scope.UserEmail})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	var mutex sync.Mutex
	success, conflict := 0, 0
	for index := 0; index < 6; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, saveErr := store.SaveCustomer(scope, customer)
			mutex.Lock()
			defer mutex.Unlock()
			if saveErr == nil {
				success++
			} else if errors.Is(saveErr, ErrRecruitmentVersion) {
				conflict++
			} else {
				t.Error(saveErr)
			}
		}()
	}
	wait.Wait()
	if success != 1 || conflict != 5 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
