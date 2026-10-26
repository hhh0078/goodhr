// 本文件仅连接显式指定的回环_test数据库，在独立schema验证筛选记录迁移、事务幂等、SQL范围和数据隔离。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// screeningPostgresFixture 复用带回环和库名保护的测试库，为筛选记录补齐最小真实岗位所有权结构。
func screeningPostgresFixture(t *testing.T) (*sql.DB, ScreeningScope, ScreeningScope) {
	t.Helper()
	db := candidatePostgresTestDB(t)
	db.SetMaxOpenConns(10)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'; ALTER TABLE positions ADD COLUMN user_id UUID REFERENCES users(id);`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0103_position_screening_records.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatalf("migration apply %d: %v", index, err)
		}
	}
	var scope, other ScreeningScope
	scope.UserEmail = "screening-owner@example.com"
	other.UserEmail = "screening-outsider@example.com"
	if err = db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&scope.TenantID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&other.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2),('screening-member@example.com',$2),($3,$4)`, scope.UserEmail, scope.TenantID, other.UserEmail, other.TenantID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO positions(name,user_id) SELECT '本人岗位',id FROM users WHERE email=$1 RETURNING id::text`, scope.UserEmail).Scan(&scope.PositionID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO positions(name,user_id) SELECT '外部岗位',id FROM users WHERE email=$1 RETURNING id::text`, other.UserEmail).Scan(&other.PositionID); err != nil {
		t.Fatal(err)
	}
	var withoutComment int
	if err = db.QueryRow(`SELECT count(*) FROM pg_attribute a WHERE a.attrelid='position_screening_records'::regclass AND a.attnum>0 AND NOT a.attisdropped AND col_description(a.attrelid,a.attnum) IS NULL`).Scan(&withoutComment); err != nil || withoutComment != 0 {
		t.Fatalf("missing column comments=%d err=%v", withoutComment, err)
	}
	return db, scope, other
}

// TestScreeningPostgresContract 真实执行全部快照版本、并发、四类SQL范围及零分空值合同。
func TestScreeningPostgresContract(t *testing.T) {
	db, scope, _ := screeningPostgresFixture(t)
	screeningStoreContract(t, NewPostgresScreeningRecordStore(db), scope)
}

// TestScreeningPostgresIsolation 验证普通同事、外部租户、岗位转交、成员换租户和失效成员均不能借旧记录读写。
func TestScreeningPostgresIsolation(t *testing.T) {
	db, scope, other := screeningPostgresFixture(t)
	store := NewPostgresScreeningRecordStore(db)
	ctx := context.Background()
	snapshot := screeningTestSnapshot(0)
	if _, err := store.Save(ctx, scope, snapshot); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ScreeningScope{{TenantID: scope.TenantID, PositionID: scope.PositionID, UserEmail: "screening-member@example.com"}, {TenantID: other.TenantID, PositionID: scope.PositionID, UserEmail: scope.UserEmail}, {TenantID: scope.TenantID, PositionID: other.PositionID, UserEmail: scope.UserEmail}, {TenantID: other.TenantID, PositionID: scope.PositionID, UserEmail: other.UserEmail}, {PositionID: scope.PositionID, UserEmail: scope.UserEmail}} {
		if _, err := store.Save(ctx, bad, snapshot); !errors.Is(err, ErrNotFound) {
			t.Fatalf("bad write scope=%+v err=%v", bad, err)
		}
		if _, err := store.List(ctx, bad, ScreeningRecordQuery{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("bad read scope=%+v err=%v", bad, err)
		}
	}
	if _, err := db.Exec(`UPDATE positions SET user_id=(SELECT id FROM users WHERE email='screening-member@example.com') WHERE id=$1`, scope.PositionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, scope, ScreeningRecordQuery{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old owner read=%v", err)
	}
	member := scope
	member.UserEmail = "screening-member@example.com"
	list, err := store.List(ctx, member, ScreeningRecordQuery{})
	if err != nil || list.Total != 0 {
		t.Fatalf("new owner read old sensitive rows=%+v err=%v", list, err)
	}
	if _, err = store.Save(ctx, member, snapshot); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new owner overwrite old attempt=%v", err)
	}
	if _, err = db.Exec(`UPDATE positions SET user_id=(SELECT id FROM users WHERE email=$1) WHERE id=$2;`, scope.UserEmail, scope.PositionID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE users SET tenant_id=$1 WHERE email=$2`, other.TenantID, scope.UserEmail); err != nil {
		t.Fatal(err)
	}
	if _, err = store.List(ctx, scope, ScreeningRecordQuery{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old tenant read=%v", err)
	}
	moved := scope
	moved.TenantID = other.TenantID
	list, err = store.List(ctx, moved, ScreeningRecordQuery{})
	if err != nil || list.Total != 0 {
		t.Fatalf("moved tenant old record=%+v err=%v", list, err)
	}
	if _, err = db.Exec(`UPDATE users SET status='inactive' WHERE email=$1`, scope.UserEmail); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Save(ctx, moved, screeningTestSnapshot(1)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive member write=%v", err)
	}
}

// TestScreeningPostgresConcurrentFirstWrite 验证首次离线上报并发重传仅插入一条且回执主键一致，相同时间分页稳定。
func TestScreeningPostgresConcurrentFirstWrite(t *testing.T) {
	db, scope, _ := screeningPostgresFixture(t)
	store := NewPostgresScreeningRecordStore(db)
	ctx := context.Background()
	snapshot := screeningTestSnapshot(0)
	var wg sync.WaitGroup
	ids := sync.Map{}
	errs := make(chan error, 20)
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := store.Save(ctx, scope, snapshot)
			if err != nil {
				errs <- err
				return
			}
			ids.Store(item.ID, true)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	count := 0
	ids.Range(func(key, value any) bool { count++; return true })
	if count != 1 {
		t.Fatalf("first write ids=%d", count)
	}
	for index := 1; index <= 25; index++ {
		item := screeningTestSnapshot(index)
		item.OccurredAt = snapshot.OccurredAt
		item.CandidateKey = snapshot.CandidateKey
		if _, err := store.Save(ctx, scope, item); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.List(ctx, scope, ScreeningRecordQuery{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.List(ctx, scope, ScreeningRecordQuery{Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	items := append(first.Records, second.Records...)
	if len(items) != 26 || first.Total != 26 {
		t.Fatalf("pages=%d total=%d", len(items), first.Total)
	}
	for index := 1; index < len(items); index++ {
		if items[index-1].AttemptID <= items[index].AttemptID {
			t.Fatal("same-time sort is unstable")
		}
	}
}
