// 本文件用真实 PostgreSQL 行锁验证人工岗位保存与补充归档互斥，以及公司上下文变化后旧分类失效。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// waitEditableFactBlocked 等到数据库确认目标事务被指定会话阻塞，不依赖固定延时猜测执行进度。
func waitEditableFactBlocked(t *testing.T, ctx context.Context, db *sql.DB, blocker int, finished <-chan error) int {
	t.Helper()
	for {
		select {
		case err := <-finished:
			t.Fatalf("事务没有等待应共享的配置锁，提前结束：%v", err)
		case <-ctx.Done():
			t.Fatal("等待数据库锁超时", ctx.Err())
		default:
		}
		var pid int
		err := db.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) LIMIT 1`, blocker).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestEditableFactsManualSaveSerializesArchive 验证人工保存读版本后暂停时，归档必须等待并拒绝旧模型快照。
func TestEditableFactsManualSaveSerializesArchive(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	draft, err := f.store.GetPositionAutoReplyConfig(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	draft.PositionDescription = "人工核实后的岗位安排"
	q, claimed, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, f.question.ID, "此前的岗位安排", f.question.Version)
	if err != nil || !claimed {
		t.Fatal("领取归档失败", err)
	}
	snapshot, err := f.store.questionFactContext(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	// 卡住话术库读取，使实际保存方法已经通过版本核对但还没有写入正文。
	block, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	var blocker int
	var library string
	if err = block.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	if err = block.QueryRowContext(ctx, `SELECT config_value::text FROM system_configs WHERE config_key=$1 FOR UPDATE`, autoReplyLibraryKey).Scan(&library); err != nil {
		t.Fatal(err)
	}
	saved := make(chan error, 1)
	go func() {
		_, err := f.store.SavePositionAutoReplyConfig(ctx, "fact-hr@example.test", draft)
		saved <- err
	}()
	savePID := waitEditableFactBlocked(t, ctx, f.db, blocker, saved)
	archived := make(chan error, 1)
	go func() {
		_, err := f.store.finishQuestionAnswer(ctx, f.tenant, f.pos, q.ID, q.Version, f.company, "position", "此前的岗位安排", "", snapshot)
		archived <- err
	}()
	waitEditableFactBlocked(t, ctx, f.db, savePID, archived)
	if err = block.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-saved; err != nil {
		t.Fatal("人工保存失败或产生死锁", err)
	}
	if err = <-archived; !errors.Is(err, errReplyStateConflict) {
		t.Fatal("旧归档没有因人工修改而冲突", err)
	}
	current, err := f.store.GetPositionAutoReplyConfig(ctx, f.tenant, f.pos)
	if err != nil || current.PositionDescription != draft.PositionDescription || current.Version != draft.Version+1 {
		t.Fatal("旧分类覆盖了人工正文", current.PositionDescription, err)
	}
}

// TestEditableFactsArchiveRejectsWaitingOldDraft 验证归档先锁住配置时，等候的旧表单在读到新版本后拒绝保存。
func TestEditableFactsArchiveRejectsWaitingOldDraft(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	draft, err := f.store.GetPositionAutoReplyConfig(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.store.questionFactContext(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var archivePID int
	if err = tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&archivePID); err != nil {
		t.Fatal(err)
	}
	if err = applyQuestionFact(ctx, tx, f.tenant, f.pos, f.company, "position", "新核实的岗位事实", "", "", "", []replyFactContext{snapshot}); err != nil {
		t.Fatal(err)
	}
	saved := make(chan error, 1)
	go func() {
		_, err := f.store.SavePositionAutoReplyConfig(ctx, "fact-hr@example.test", draft)
		saved <- err
	}()
	waitEditableFactBlocked(t, ctx, f.db, archivePID, saved)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-saved; err == nil {
		t.Fatal("等待期间已过期的岗位草稿覆盖了新归档")
	}
	current, err := f.store.GetPositionAutoReplyConfig(ctx, f.tenant, f.pos)
	if err != nil || current.PositionDescription != mergeEditableReplyFact(draft.PositionDescription, "", "新核实的岗位事实") {
		t.Fatal("新归档事实被旧草稿覆盖", current.PositionDescription, err)
	}
}

// TestEditableFactsPositionArchiveChecksCompanyContext 验证公司被人工改为待确认后，只写岗位的旧分类也不能重新发布被撤回的事实。
func TestEditableFactsPositionArchiveChecksCompanyContext(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx := context.Background()
	q, claimed, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, f.question.ID, "公司双休", f.question.Version)
	if err != nil || !claimed {
		t.Fatal("领取归档失败", err)
	}
	snapshot, err := f.store.questionFactContext(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil {
		t.Fatal(err)
	}
	company.ExtraInfo = "休息安排待确认"
	if _, err = f.store.SaveCompanyProfile(ctx, f.tenant, "fact-hr@example.test", company); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.finishQuestionAnswer(ctx, f.tenant, f.pos, q.ID, q.Version, f.company, "position", "公司双休", "", snapshot); !errors.Is(err, errReplyStateConflict) {
		t.Fatal("旧公司上下文仍被归档进岗位", err)
	}
	current, err := f.store.GetPositionAutoReplyConfig(ctx, f.tenant, f.pos)
	if err != nil || current.PositionDescription != snapshot.PositionDescription {
		t.Fatal("旧分类修改了岗位正文", current.PositionDescription, err)
	}
}

// TestEditableFactsConcurrentCompanyArchivesDoNotUpgradeSharedLocks 验证同岗位两条公司归档串行取得配置锁，过期者安全冲突而非死锁。
func TestEditableFactsConcurrentCompanyArchivesDoNotUpgradeSharedLocks(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f.entity(t, "messages", "parking-source", map[string]string{"conversation_id": "thread", "direction": "candidate", "text_content": "公司提供停车服务吗？", "platform_message_id": "parking-key"})
	second, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, "", "公司提供停车服务吗", "thread", "parking-key")
	if err != nil {
		t.Fatal(err)
	}
	first, claimed, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, f.question.ID, "公司办公地址在成都A座。", f.question.Version)
	if err != nil || !claimed {
		t.Fatal("第一条问题未领取", err)
	}
	second, claimed, err = f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, second.ID, "公司提供停车服务。", second.Version)
	if err != nil || !claimed {
		t.Fatal("第二条问题未领取", err)
	}
	snapshot, err := f.store.questionFactContext(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	block, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	var blocker int
	if err = block.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	// 在首次配置锁与资料写入之间挡住来源核验，使旧实现稳定暴露两个共享锁升级的互等。
	if _, err = block.ExecContext(ctx, `LOCK TABLE auto_reply_cloud_question_sources IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	archive := func(q replyQuestion, fact string, done chan<- error) {
		_, err := f.store.finishQuestionAnswer(ctx, f.tenant, f.pos, q.ID, q.Version, f.company, "company", fact, "", snapshot)
		done <- err
	}
	go archive(first, "公司办公地址在成都A座。", firstDone)
	firstPID := waitEditableFactBlocked(t, ctx, f.db, blocker, firstDone)
	go archive(second, "公司提供停车服务。", secondDone)
	for {
		var pid int
		err = f.db.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND pid<>$1 AND ($1=ANY(pg_blocking_pids(pid)) OR $2=ANY(pg_blocking_pids(pid))) LIMIT 1`, firstPID, blocker).Scan(&pid)
		if err == nil {
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("等待第二条并发归档失败", err)
		}
		select {
		case err = <-secondDone:
			t.Fatal("第二条归档没有等待同岗位锁", err)
		case <-ctx.Done():
			t.Fatal("第二条并发归档未进入预期锁等待", ctx.Err())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = block.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-firstDone; err != nil {
		t.Fatal("第一条归档发生死锁或未成功", err)
	}
	if err = <-secondDone; !errors.Is(err, errReplyStateConflict) {
		t.Fatal("并发旧公司快照应冲突，不能死锁或覆盖新资料", err)
	}
	company, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || company.ExtraInfo != mergeEditableReplyFact(snapshot.Company.ExtraInfo, "", "公司办公地址在成都A座。") {
		t.Fatal("并发公司归档没有保留先完成的资料", company.ExtraInfo, err)
	}
}
