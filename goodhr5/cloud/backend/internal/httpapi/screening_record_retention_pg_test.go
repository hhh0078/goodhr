// 本文件在回环隔离PostgreSQL验证500条保留窗口、不同候选人的并发写入和岗位转交后的总量限制。
package httpapi

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestScreeningPostgresRetention 以真实数据库覆盖第501条、重传、更新、晚到记录和范围隔离。
func TestScreeningPostgresRetention(t *testing.T) {
	t.Run("顺序与隔离", func(t *testing.T) {
		db, scope, other := screeningPostgresFixture(t)
		another := scope
		if err := db.QueryRow(`INSERT INTO positions(name,user_id) SELECT '另一个本人岗位',id FROM users WHERE email=$1 RETURNING id::text`, scope.UserEmail).Scan(&another.PositionID); err != nil {
			t.Fatal(err)
		}
		screeningRetentionChronological(t, NewPostgresScreeningRecordStore(db), scope, another, other)
	})
	t.Run("相同时间旧记录重放", func(t *testing.T) {
		db, scope, _ := screeningPostgresFixture(t)
		screeningRetentionSameTime(t, NewPostgresScreeningRecordStore(db), scope)
	})
}

// TestScreeningPostgresRetentionConcurrent 验证同岗位不同attempt的并发提交始终最多500条，最后精确保留最新集合。
func TestScreeningPostgresRetentionConcurrent(t *testing.T) {
	db, scope, other := screeningPostgresFixture(t)
	store := NewPostgresScreeningRecordStore(db)
	screeningRetentionSave(t, store, other, screeningTestSnapshot(0))
	for index := 0; index < 490; index++ {
		screeningRetentionSave(t, store, scope, screeningTestSnapshot(index))
	}
	start := make(chan struct{})
	errs := make(chan error, 32)
	var workers sync.WaitGroup
	for index := 490; index < 522; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			_, err := store.Save(context.Background(), scope, screeningTestSnapshot(index))
			if err == nil {
				var count int
				err = db.QueryRow(`SELECT count(*) FROM position_screening_records WHERE tenant_id=$1::uuid AND position_id=$2::uuid`, scope.TenantID, scope.PositionID).Scan(&count)
				if err == nil && count > 500 {
					err = fmt.Errorf("并发提交后可见记录=%d，超过500条", count)
				}
			}
			errs <- err
		}(index)
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	screeningRetentionRange(t, store, scope, 22, 521)
	if got := screeningRetentionRows(t, store, other); len(got) != 1 {
		t.Fatal("并发淘汰误删了其他租户的记录")
	}
}

// TestScreeningPostgresRetentionCountsFormerOwner 验证岗位转交后旧所有者的不可见记录仍计入同岗位500条总量。
func TestScreeningPostgresRetentionCountsFormerOwner(t *testing.T) {
	db, scope, _ := screeningPostgresFixture(t)
	store := NewPostgresScreeningRecordStore(db)
	for index := 0; index < 500; index++ {
		screeningRetentionSave(t, store, scope, screeningTestSnapshot(index))
	}
	if _, err := db.Exec(`UPDATE positions SET user_id=(SELECT id FROM users WHERE email='screening-member@example.com') WHERE id=$1::uuid`, scope.PositionID); err != nil {
		t.Fatal(err)
	}
	member := scope
	member.UserEmail = "screening-member@example.com"
	latest := screeningTestSnapshot(500)
	screeningRetentionSave(t, store, member, latest)
	var count, oldest int
	if err := db.QueryRow(`SELECT count(*),count(*) FILTER (WHERE attempt_id=$3::uuid) FROM position_screening_records WHERE tenant_id=$1::uuid AND position_id=$2::uuid`, scope.TenantID, scope.PositionID, screeningTestSnapshot(0).AttemptID).Scan(&count, &oldest); err != nil || count != 500 || oldest != 0 {
		t.Fatalf("跨所有者岗位保留错误: count=%d oldest=%d err=%v", count, oldest, err)
	}
	visible := screeningRetentionRows(t, store, member)
	if len(visible) != 1 || visible[0].AttemptID != latest.AttemptID {
		t.Fatal("岗位转交后泄露旧所有者记录或未保存新记录")
	}
}
