// Package download 文件作用：验证自动回复认领本次附件时，旧下载及其他用户操作仍能正常进入手动导入。
package download

import (
	"slices"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

const receiptPage = "https://platform.example/chat"
const receiptFile = "https://files.example/resume.pdf"

// receiptRecord 创建一份带原始页面、下载链接和开始时间的原生下载快照。
func receiptRecord(id, page, file, status string, created time.Time) contract.DownloadRecord {
	return contract.DownloadRecord{ID: id, PageURL: page, URL: file, Status: status, CreatedAt: created.Format(time.RFC3339Nano)}
}

// manualSavedIDs 只提取手动简历导入实际接受的终态，pending 不会入库。
func manualSavedIDs(records []contract.DownloadRecord) []string {
	var ids []string
	for _, record := range records {
		if record.Status == "saved" {
			ids = append(ids, record.ID)
		}
	}
	return ids
}

// TestDownloadReceiptClaimsOnlyNewMatchingTerminal 验证旧记录、其他页面同链接及并发手动下载不被认领，终态重复上报不会进入手动队列。
func TestDownloadReceiptClaimsOnlyNewMatchingTerminal(t *testing.T) {
	m := &Monitor{}
	stream, stop, err := m.WatchDownload(receiptPage, receiptFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	now := time.Now()
	records := []contract.DownloadRecord{
		receiptRecord("old", receiptPage, receiptFile, "saved", now.Add(-time.Hour)),
		receiptRecord("other-page", receiptPage+"/other", receiptFile, "saved", now),
		receiptRecord("manual", receiptPage, receiptFile+"?manual", "pending", now),
		receiptRecord("target", receiptPage, receiptFile, "pending", now),
	}
	initial := manualSavedIDs(m.receipts.deliver(records))
	if !slices.Equal(initial, []string{"old", "other-page"}) {
		t.Fatalf("其他下载的手动流程被拦截：%v", initial)
	}
	select {
	case receipt := <-stream:
		if receipt.Err != nil || receipt.Record.ID != "target" || receipt.Record.Status != "pending" {
			t.Fatalf("没有先通知本次下载开始：%+v", receipt)
		}
	default:
		t.Fatal("未投递下载开始通知")
	}

	records[2].Status = "saved"
	records[3].Status = "saved"
	want := []string{"old", "other-page", "manual"}
	for range 2 {
		if got := manualSavedIDs(m.receipts.deliver(records)); !slices.Equal(got, want) {
			t.Fatalf("重复终态泄露到手动队列或吞掉其他文件：%v", got)
		}
	}
	select {
	case result := <-stream:
		if result.Err != nil || result.Record.ID != "target" {
			t.Fatalf("未返回本次附件：%+v", result)
		}
	default:
		t.Fatal("没有收到终态")
	}
	select {
	case receipt := <-stream:
		t.Fatalf("同一终态返回了两次：%+v", receipt)
	default:
	}
}

// TestDownloadReceiptRejectsAmbiguousMatch 验证同页同链接不能同时订阅；多个下载同时符合时也不猜测归属。
func TestDownloadReceiptRejectsAmbiguousMatch(t *testing.T) {
	m := &Monitor{}
	stream, stop, err := m.WatchDownload(receiptPage, receiptFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, _, err = m.WatchDownload(receiptPage, receiptFile); err == nil {
		t.Fatal("同页同链接订阅了两次")
	}
	now := time.Now()
	m.receipts.deliver([]contract.DownloadRecord{
		receiptRecord("first", receiptPage, receiptFile, "saved", now),
		receiptRecord("second", receiptPage, receiptFile, "pending", now),
	})
	select {
	case result := <-stream:
		if result.Err == nil || result.Record.ID != "" {
			t.Fatalf("多份下载错误认领：%+v", result)
		}
	default:
		t.Fatal("归属冲突没有回执")
	}
}

// TestDownloadReceiptCancellationBeforeAnyDownload 验证未见任何下载即取消后，后来用户独立发起的同页同链接下载归手动队列。
func TestDownloadReceiptCancellationBeforeAnyDownload(t *testing.T) {
	m := &Monitor{}
	stream, stop, err := m.WatchDownload(receiptPage, receiptFile)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	stop()
	got := manualSavedIDs(m.receipts.deliver([]contract.DownloadRecord{receiptRecord("manual-later", receiptPage, receiptFile, "saved", time.Now())}))
	if !slices.Equal(got, []string{"manual-later"}) {
		t.Fatalf("取消后占用了后来文件：%v", got)
	}
	select {
	case result := <-stream:
		t.Fatalf("取消的订阅仍在接收结果：%+v", result)
	default:
	}
}

// TestDownloadReceiptCancellationAfterPending 验证已见的自动下载在取消后才落盘时仍不进入手动导入。
func TestDownloadReceiptCancellationAfterPending(t *testing.T) {
	m := &Monitor{}
	_, stop, err := m.WatchDownload(receiptPage, receiptFile)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.receipts.deliver([]contract.DownloadRecord{receiptRecord("observed", receiptPage, receiptFile, "pending", now)})
	stop()
	out := m.receipts.deliver([]contract.DownloadRecord{
		receiptRecord("observed", receiptPage, receiptFile, "saved", now),
		receiptRecord("later", receiptPage, receiptFile, "saved", now.Add(time.Second)),
	})
	if got := manualSavedIDs(out); !slices.Equal(got, []string{"later"}) {
		t.Fatalf("取消后下载归属错误：%v", got)
	}
}

// TestDownloadReceiptSelectsSignedObject 验证仅签名和展示模式可变化，文件路径、来源、租户和其余参数均按原对象核对。
func TestDownloadReceiptSelectsSignedObject(t *testing.T) {
	const preview = "https://files.example:443/a/resume.pdf?object=one&sig=first&display=view"
	const saved = "https://files.example:443/a/resume.pdf?display=save&sig=second&object=one"
	m := &Monitor{}
	stream, stop, err := m.WatchDownload(receiptPage, preview, "sig", "display")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	now := time.Now()
	var records []contract.DownloadRecord
	for _, other := range []string{
		"http://files.example:443/a/resume.pdf?object=one",
		"https://other.example:443/a/resume.pdf?object=one",
		"https://files.example:444/a/resume.pdf?object=one",
		"https://files.example:443/b/resume.pdf?object=one",
		"https://files.example:443/a/resume.pdf?object=two",
		"https://files.example:443/a/resume.pdf?object=one&extra=1",
		"https://files.example:443/a/resume.pdf?object=one&object=two",
		"https://files.example:443/a/resume.pdf?object=one&sig=bad%zz",
	} {
		records = append(records, receiptRecord(other, receiptPage, other, "saved", now))
	}
	manual := len(records)
	records = append(records, receiptRecord("target", receiptPage, saved, "saved", now))
	if got := m.receipts.deliver(records); len(got) != manual {
		t.Fatalf("其它下载被消费或目标重复导入：%d", len(got))
	}
	select {
	case v := <-stream:
		if v.Err != nil || v.Record.ID != "target" {
			t.Fatal("认领了不同文件", v.Err)
		}
	default:
		t.Fatal("没有匹配同一对象的新签名")
	}

	strict, cancel, err := m.WatchDownload(receiptPage, preview)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	m.receipts.deliver([]contract.DownloadRecord{receiptRecord("strict", receiptPage, saved, "saved", time.Now())})
	select {
	case <-strict:
		t.Fatal("精确订阅忽略了查询参数")
	default:
	}
}

// TestDownloadReceiptRejectsAmbiguousSignedURL 验证匹配规则有交集时拒绝二次登记，损坏或归属不明的链接不能登记。
func TestDownloadReceiptRejectsAmbiguousSignedURL(t *testing.T) {
	for _, raw := range []string{"https://files.example", "https://files.example/", receiptFile + "?sig=a&sig=b", receiptFile + "?file=one;file=two", receiptFile + "?sig=%z0", receiptFile + "?=one", receiptFile + "#other", "https://user@files.example/resume.pdf", "https://files.example/a/../resume.pdf"} {
		if _, _, err := new(Monitor).WatchDownload(receiptPage, raw, "sig"); err == nil {
			t.Fatal("接收了有歧义的订阅", raw)
		}
	}
	m := &Monitor{}
	_, stop, err := m.WatchDownload(receiptPage, receiptFile+"?id=1&sig=a&mode=one", "sig")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for _, item := range []struct {
		raw     string
		ignored []string
	}{
		{receiptFile + "?id=1&sig=b&mode=one", nil},
		{receiptFile + "?id=1&sig=c&mode=one", []string{"sig"}},
		{receiptFile + "?id=1&sig=c&mode=two", []string{"mode"}},
	} {
		if _, _, err := m.WatchDownload(receiptPage, item.raw, item.ignored...); err == nil {
			t.Fatal("允许存在重叠的订阅", item)
		}
	}
	_, other, err := m.WatchDownload(receiptPage, receiptFile+"?id=2&sig=b&mode=one", "sig")
	if err != nil {
		t.Fatal("拦截了不同对象", err)
	}
	other()
}

// TestDownloadReceiptBuffersSinglePendingAndTerminal 验证页面点击较慢、消费者尚未开始读时，同个下载的在途只通知一次且终态仍能入队。
func TestDownloadReceiptBuffersSinglePendingAndTerminal(t *testing.T) {
	m := &Monitor{}
	stream, stop, err := m.WatchDownload(receiptPage, receiptFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	record := receiptRecord("target", receiptPage, receiptFile, "pending", time.Now())
	if cap(stream) < 2 {
		t.Fatal("监听应为首次在途通知和最终结果都保留位置")
	}
	for range 3 {
		m.receipts.deliver([]contract.DownloadRecord{record})
		if len(stream) != 1 {
			t.Fatalf("在途记录被重复或遗漏：%d", len(stream))
		}
	}
	record.Status = "saved"
	m.receipts.deliver([]contract.DownloadRecord{record})
	for _, status := range []string{"pending", "saved"} {
		select {
		case result := <-stream:
			if result.Err != nil || result.Record.ID != "target" || result.Record.Status != status {
				t.Fatalf("在途或终态通知不正确：%+v", result)
			}
		default:
			t.Fatal("缺少通知", status)
		}
	}
	select {
	case extra := <-stream:
		t.Fatal("重复通知", extra)
	default:
	}
}

// TestDownloadReceiptKeepsPendingIdentity 验证原在途编号消失时，不能误认随后出现的另一份相同链接文件。
func TestDownloadReceiptKeepsPendingIdentity(t *testing.T) {
	m := &Monitor{}
	stream, stop, err := m.WatchDownload(receiptPage, receiptFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	now := time.Now()
	m.receipts.deliver([]contract.DownloadRecord{receiptRecord("original", receiptPage, receiptFile, "pending", now)})
	<-stream
	manual := m.receipts.deliver([]contract.DownloadRecord{receiptRecord("replacement", receiptPage, receiptFile, "saved", now)})
	select {
	case receipt := <-stream:
		if receipt.Err == nil || !slices.Equal(manualSavedIDs(manual), []string{"replacement"}) {
			t.Fatal("错误认领其它下载")
		}
	default:
		t.Fatal("没有报告在途编号变化")
	}
}
