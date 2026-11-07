// Package cloud 本文件验证全部联系人扫描基线的账号隔离、持久待办、删除边界及乱序回执。
package cloud

import (
	"context"
	"fmt"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// TestAutoReplyListRecoveryAndABA 验证未读红点不是必需，重启后待办保留，同分钟同文回到旧内容时旧确认无效。
func TestAutoReplyListRecoveryAndABA(t *testing.T) {
	ctx := context.Background()
	c, cred, store := entityTestClient(t)
	start := time.Now().UTC().Truncate(time.Minute).Add(20 * time.Second)
	row := AutoReplyListObservation{ThreadID: "private-thread", Signature: "old", PreviewDigest: autoReplyDigest("old")}
	scan := func(at int, rows ...AutoReplyListObservation) []AutoReplyListReview {
		t.Helper()
		v, e := c.ObserveAutoReplyList(ctx, cred, "liepin", "acct", "", rows, start.Add(time.Duration(at)*time.Second))
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	if v := scan(0, row); len(v) != 0 {
		t.Fatal("初次旧线程误入待办")
	}
	row.Signature = "new"
	row.PreviewDigest = autoReplyDigest("new")
	first := scan(1, row)
	if len(first) != 1 || first[0].WitnessDigest != row.PreviewDigest || !first[0].After.Equal(start) {
		t.Fatal("未识别无红点的摘要变化", first)
	}
	c = New("")
	c.EnableLocalAutoReply(store, t.TempDir())
	if again := scan(2, row); len(again) != 1 || again[0] != first[0] {
		t.Fatal("重启丢失已登记的待办", again)
	}
	row.Signature, row.PreviewDigest = "old", autoReplyDigest("old")
	_ = scan(3, row)
	row.Signature, row.PreviewDigest = "new", autoReplyDigest("new")
	last := scan(4, row)
	if e := c.AckAutoReplyList(ctx, cred, "liepin", "another-account", "", last[0]); e == nil {
		t.Fatal("跨账号回执被接受")
	}
	if e := c.AckAutoReplyList(ctx, cred, "liepin", "acct", row.ThreadID, last[0]); e == nil {
		t.Fatal("跨限定模式回执被接受")
	}
	if len(last) != 1 || first[0].Revision == last[0].Revision || !last[0].After.Equal(start) {
		t.Fatal("A→B→A 的版本或保护边界错误")
	}
	if e := c.AckAutoReplyList(ctx, cred, "liepin", "acct", "", first[0]); e != nil {
		t.Fatal(e)
	}
	if v := scan(5, row); len(v) != 1 || v[0].Revision != last[0].Revision {
		t.Fatal("旧确认清空了新消息", v)
	}
	if e := c.AckAutoReplyList(ctx, cred, "liepin", "acct", "", last[0]); e != nil {
		t.Fatal(e)
	}
	if v := scan(6, row); len(v) != 0 {
		t.Fatal("成功确认后重复核对")
	}
	added := AutoReplyListObservation{ThreadID: "new-thread", Signature: "added", PreviewDigest: autoReplyDigest("added")}
	v := scan(7, row, added)
	if len(v) != 1 || v[0].ThreadID != added.ThreadID || v[0].WitnessDigest != added.PreviewDigest || !v[0].After.Equal(start.Add(6*time.Second)) {
		t.Fatal("启动后新增线程未登记", v)
	}
	row.Signature = "not-committed"
	if _, e := c.ObserveAutoReplyList(ctx, cred, "liepin", "acct", "", []AutoReplyListObservation{row, row}, start.Add(8*time.Second)); e == nil {
		t.Fatal("重复线程未拒绝")
	}
	row.Signature = "new"
	if v := scan(9, row, added); len(v) != 1 || v[0].ThreadID != added.ThreadID {
		t.Fatal("不完整读数覆盖了完整水位或泄露部分更新", v)
	}
}

// TestAutoReplyListIsolatesModesOwnersAndDeletesSource 验证普通及单人范围互不污染，精确删除同一线程的两份基线。
func TestAutoReplyListIsolatesModesOwnersAndDeletesSource(t *testing.T) {
	ctx := context.Background()
	c, _, cred := purgeTestClient(t, "")
	start := time.Now().UTC()
	a := AutoReplyListObservation{ThreadID: "one", Signature: "A", PreviewDigest: autoReplyDigest("A")}
	b := AutoReplyListObservation{ThreadID: "two", Signature: "B", PreviewDigest: autoReplyDigest("B")}
	observe := func(owner AgentCredentials, plat, acct, scope string, offset int, items ...AutoReplyListObservation) []AutoReplyListReview {
		t.Helper()
		v, err := c.ObserveAutoReplyList(ctx, owner, plat, acct, scope, items, start.Add(time.Second*time.Duration(offset)))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	_ = observe(cred, "liepin", "acct", "one", 0, b)
	if v := observe(cred, "liepin", "acct", "one", 1, a, b); len(v) != 1 || v[0].ThreadID != a.ThreadID {
		t.Fatal("限定范围异常", v)
	}
	for _, ids := range [][]string{{"liepin", "acct", ""}, {"liepin", "acct2", ""}, {"boss", "acct", ""}} {
		if v := observe(cred, ids[0], ids[1], ids[2], 1, a, b); len(v) != 0 {
			t.Fatal("平台、账号或限定缓存串用", v)
		}
	}
	other := cred
	other.Token = "other"
	if v := observe(other, "liepin", "acct", "", 5, a, b); len(v) != 0 {
		t.Fatal("共享了其他所有者的水位", v)
	}
	if e := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		id := originalContactID(AutoReplyCandidateLookup{PlatformID: "liepin", PlatformAccountID: "acct", PlatformThreadID: "one"})
		if e := tx.Put("conversations", id, AutoReplyConversation{ID: id, PlatformID: "liepin", PlatformAccountID: "acct", PlatformThreadID: "one", CandidateID: "candidate-one", LastSyncedMessageKey: "synced"}); e != nil {
			return e
		}
		return tx.Put("cloud_links", "candidate:candidate-one", localCloudLink{Value: "cloud-one"})
	}); e != nil {
		t.Fatal(e)
	}
	if e := c.PurgeLocalAutoReplyCandidate(ctx, cred, "cloud-one", nil); e != nil {
		t.Fatal(e)
	}
	a.Signature = "after-purge"
	if v := observe(cred, "liepin", "acct", "one", 10, a, b); len(v) != 0 {
		t.Fatal("已删除目标在限定模式仍残留待办", v)
	}
	b.Signature = "changed"
	if v := observe(cred, "liepin", "acct", "", 10, a, b); len(v) != 1 || v[0].ThreadID != b.ThreadID {
		t.Fatal("删除后旧线程误当新联系人或误删邻居", v)
	}
	if v := observe(other, "liepin", "acct", "", 11, a, b); len(v) != 2 {
		t.Fatal("删除波及其他用户", v)
	}
}

// TestAutoReplyListKnownCursorAndQuietProbe 验证已有聊天游标可从第一次快照恢复，轮转只选一个已核对的活跃线程。
func TestAutoReplyListKnownCursorAndQuietProbe(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	now := time.Now()
	items := []AutoReplyListObservation{{ThreadID: "known", Signature: "one"}, {ThreadID: "stranger", Signature: "two"}}
	_, err := c.SaveAutoReplyConversation(ctx, cred, AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "acct", PlatformThreadID: "known", LastSyncedMessageKey: "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	scan := func(tick time.Time) []AutoReplyListReview {
		t.Helper()
		v, e := c.ObserveAutoReplyList(ctx, cred, "liepin", "acct", "", items, tick)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	v := scan(now)
	if len(v) != 1 || v[0].ThreadID != "known" || !v[0].After.IsZero() {
		t.Fatal("已有游标首次不能增量核对", v)
	}
	if e := c.AckAutoReplyList(ctx, cred, "liepin", "acct", "", v[0]); e != nil {
		t.Fatal(e)
	}
	if v = scan(now.Add(time.Second)); len(v) != 0 {
		t.Fatal("刚核对就重复打开", v)
	}
	if v = scan(now.Add(61 * time.Second)); len(v) != 1 || v[0].ThreadID != "known" || v[0].WitnessDigest != "" {
		t.Fatal("同一分钟同文兜底错误", v)
	}
}

// TestAutoReplyListRotatesBehindBusyFrontRows 验证限额一条时第十三位之后的同文线程也获名额，频繁来信、旧待办和陌生人互不挤占。
func TestAutoReplyListRotatesBehindBusyFrontRows(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Minute)
	rows := make([]AutoReplyListObservation, 15)
	for i := range rows {
		rows[i] = AutoReplyListObservation{ThreadID: fmt.Sprintf("row-%d", i), Signature: "同一份预览"}
	}
	for _, i := range []int{12, 13} {
		_, err := c.SaveAutoReplyConversation(ctx, cred, AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "acct", PlatformThreadID: rows[i].ThreadID, LastSyncedMessageKey: "stable-tail"})
		if err != nil {
			t.Fatal(err)
		}
	}
	scan := func(second int) []AutoReplyListReview {
		t.Helper()
		v, err := c.ObserveAutoReplyList(ctx, cred, "liepin", "acct", "", rows, start.Add(time.Duration(second)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ack := func(v AutoReplyListReview) {
		t.Helper()
		if err := c.AckAutoReplyList(ctx, cred, "liepin", "acct", "", v); err != nil {
			t.Fatal(err)
		}
	}
	initial := scan(0)
	if len(initial) != 2 || initial[0].ThreadID != rows[12].ThreadID || initial[1].ThreadID != rows[13].ThreadID {
		t.Fatal("陌生旧人不应产生待办", initial)
	}
	for _, v := range initial {
		ack(v)
	}
	rows[1].Signature = "先来的待办"
	if v := scan(55); len(v) != 1 || v[0].ThreadID != rows[1].ThreadID {
		t.Fatal("未发现变化", v)
	}
	rows[0].Signature = "前排新的变化"
	v := scan(56)
	if len(v) != 2 || v[0].ThreadID != rows[1].ThreadID || v[1].ThreadID != rows[0].ThreadID {
		t.Fatal("前排新变化挤掉了已挂起待办", v)
	}
	ack(v[0])
	for _, step := range []struct {
		second   int
		head     int
		count    int
		reserved bool
	}{
		{59, 0, 1, false},
		{61, 12, 2, true},
		{122, 0, 1, false},
		{183, 13, 2, true},
		{188, 0, 1, false},
	} {
		rows[0].Signature = fmt.Sprintf("持续来信-%d", step.second)
		v := scan(step.second)
		if len(v) != step.count || v[0].ThreadID != rows[step.head].ThreadID || v[0].ReserveTurn != step.reserved {
			t.Fatalf("第 %d 秒的名额错误: %+v", step.second, v)
		}
		if step.reserved && (!v[0].After.IsZero() || v[0].WitnessDigest != "" || v[1].ReserveTurn) {
			t.Fatal("轮查只能使用原有游标，每轮只能给一个名额", v)
		}
		ack(v[0])
	}
}

// TestAutoReplyListReservesExistingPendingReview 验证另一项本地任务一直占满名额时，已经登记的静默核对仍获得后续优先机会。
func TestAutoReplyListReservesExistingPendingReview(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Minute)
	rows := []AutoReplyListObservation{{ThreadID: "known", Signature: "same", PreviewDigest: autoReplyDigest("hello")}, {ThreadID: "stranger", Signature: "old"}}
	_, err := c.SaveAutoReplyConversation(ctx, cred, AutoReplyConversation{PlatformID: "liepin", PlatformAccountID: "acct", PlatformThreadID: "known", LastSyncedMessageKey: "tail"})
	if err != nil {
		t.Fatal(err)
	}
	scan := func(second int) []AutoReplyListReview {
		t.Helper()
		v, err := c.ObserveAutoReplyList(ctx, cred, "liepin", "acct", "", rows, start.Add(time.Duration(second)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := scan(50)
	if len(v) != 1 || v[0].ThreadID != "known" {
		t.Fatal("首次核对范围错误", v)
	}
	if err := c.AckAutoReplyList(ctx, cred, "liepin", "acct", "", v[0]); err != nil {
		t.Fatal(err)
	}
	if v = scan(100); len(v) != 0 {
		t.Fatal("提前轮查", v)
	}
	v = scan(111)
	if len(v) != 1 || v[0].ThreadID != "known" || v[0].ReserveTurn {
		t.Fatal("非跨分钟轮次只登记静默核对", v)
	}
	// 本地恢复任务占了前面所有轮次，待核对行随后又出现了新文字。
	rows[0].Signature, rows[0].PreviewDigest = "changed", autoReplyDigest("new message")
	current := scan(115)
	if len(current) != 1 || current[0].Revision == v[0].Revision || current[0].WitnessDigest != rows[0].PreviewDigest {
		t.Fatal("挂起期间真实变化未记录", current)
	}
	v = scan(121)
	if len(v) != 1 || !v[0].ReserveTurn {
		t.Fatal("唯一已挂起的核对未获得实际名额", v)
	}
	promoted := v[0]
	promoted.ReserveTurn = false
	if promoted != current[0] {
		t.Fatal("恢复待办时改动了原核对凭据", current[0], v[0])
	}
}
