// Package resume_import 本文件用私有临时目录验证旧孤儿草稿的同文匹配和人工选择移除，不调用真实AI。
package resume_import

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// TestPurgeFourDownloadedPDFDrafts 用不同字节、相同可信正文模拟四次平台下载，同时保护重导、另一账号及改变的原文。
func TestPurgeFourDownloadedPDFDrafts(t *testing.T) {
	m, _, _, _, root := newTestManager(t, "")
	ctx := context.Background()
	before := time.Now().Add(-time.Hour)
	sameText := "示例甲 13800138001 fixture@example.invalid " + strings.Repeat("虚构工作经历和教育信息 ", 40)
	var targets, untouched []string
	var anchor string
	for i, c := range []struct {
		owner, body, source        string
		recent, otherID, wrongFile bool
	}{
		{"A", sameText, "liepin", false, false, false}, {"A", strings.ReplaceAll(sameText, " ", "\n"), "liepin", false, false, false}, {"A", sameText, "liepin", false, false, false}, {"A", sameText, "liepin", false, false, false},
		{"A", sameText + "内容不同", "liepin", false, false, false}, {"B", sameText, "liepin", false, false, false}, {"A", sameText, "liepin", true, false, false}, {"A", sameText, "other", false, false, false}, {"A", sameText + "另一次独立入库", "liepin", false, true, false}, {"A", sameText, "liepin", false, false, true},
	} {
		// 同一个PDF文本可以存在不同元数据字节，下载任务号由本地原始字节和账号派生。
		file := []byte("%PDF-1.7 fixture" + string(rune('A'+i)))
		h := sha256.Sum256(file)
		digest := hex.EncodeToString(h[:])
		taskHash := sha256.Sum256([]byte(c.owner + ":" + digest))
		id := hex.EncodeToString(taskHash[:16])
		name := "item"
		if i == 0 {
			anchor = digest
		}
		full := filepath.Join(root, id, name+".pdf")
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if c.wrongFile {
			file = []byte("different bytes")
		}
		if err := os.WriteFile(full, file, 0600); err != nil {
			t.Fatal(err)
		}
		yes := true
		record := Item{ID: name, FileName: "同名.pdf", Status: "ready", Candidate: cloud.LibraryCandidate{IsResume: &yes, StructuredCandidate: cloud.StructuredCandidate{CandidateName: "示例甲", Phone: "13800138001", Email: "fixture@example.invalid", RawText: c.body}}}
		batch := &storedTask{OwnerID: c.owner, CreatedAt: before.UnixNano(), Task: Task{ID: id, Items: []Item{record}}, Paths: map[string]string{name: full}, Sources: map[string]DownloadSource{name: {PlatformID: c.source}}}
		if c.recent {
			batch.CreatedAt = time.Now().Add(time.Hour).UnixNano()
		}
		if c.otherID {
			batch.Task.Items[0].CandidateID = "independent-cloud-id"
		}
		m.tasks[id] = batch
		if i < 4 {
			targets = append(targets, full)
		} else {
			untouched = append(untouched, full)
		}
	}
	purgeDownload := func(context.Context, string, string) error { return nil }
	if err := m.PurgeCandidateCopies(ctx, "A", "deleted", time.Now(), nil, nil, purgeDownload); err != nil {
		t.Fatal(err)
	}
	for _, p := range targets {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("无锚点时不应清理", err)
		}
	}
	// 明确另属一个云端编号的相同正文，先阻止所有靠正文推断的自动清理。
	conflict := m.tasks[filepath.Base(filepath.Dir(untouched[4]))]
	conflict.Task.Items[0].Candidate.RawText = sameText
	if err := m.PurgeCandidateCopies(ctx, "A", "deleted", time.Now(), []string{anchor}, nil, purgeDownload); err != nil {
		t.Fatal(err)
	}
	for _, p := range targets {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("冲突来源不应自动推断", err)
		}
	}
	conflict.Task.Items[0].Candidate.RawText = sameText + "另一次独立入库"
	if err := m.PurgeCandidateCopies(ctx, "A", "deleted", time.Now(), []string{anchor}, nil, purgeDownload); err != nil {
		t.Fatal(err)
	}
	for _, p := range targets {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("目标草稿仍存在")
		}
	}
	for _, p := range untouched {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("其他文件受影响", err)
		}
	}
}

// TestDiscardChecksRevisionAndResumes 先验证错账号和预览后替换文件会拒绝，再验证来源暂时占用、重启后仍可精确续清。
func TestDiscardChecksRevisionAndResumes(t *testing.T) {
	m, store, client, parser, root := newTestManager(t, "13800138001")
	ctx := context.Background()
	batch := waitProcessed(t, m, createTestTask(t, m, 2).ID)
	m.Close()
	id := batch.Items[0].ID
	other := batch.Items[1].ID
	if _, err := m.DiscardPreview("other", batch.ID, id); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	revision, err := m.DiscardPreview("owner-a", batch.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	path := m.tasks[batch.ID].Paths[id]
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.DiscardDraft(ctx, "owner-a", batch.ID, id, revision, nil); !errors.Is(err, ErrDraftChanged) {
		t.Fatal(err)
	}
	m.tasks[batch.ID].Sources = map[string]DownloadSource{id: {PlatformID: "liepin"}}
	revision, err = m.DiscardPreview("owner-a", batch.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.DiscardDraft(ctx, "owner-a", batch.ID, id, revision, func(context.Context, string, string) error { return ErrBusy }); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	restored, err := New(store, client, parser, fakeExtractor{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	next, err := restored.DiscardPreview("owner-a", batch.ID, id)
	if err != nil || next != revision {
		t.Fatalf("确认版本未恢复 %v", err)
	}
	// 在已确认的下载来源清理期间替换文件，旧确认不能继续移除这份新内容。
	_, err = restored.DiscardDraft(ctx, "owner-a", batch.ID, id, revision, func(context.Context, string, string) error {
		return os.WriteFile(path, []byte("new private copy"), 0600)
	})
	if !errors.Is(err, ErrDraftChanged) {
		t.Fatal("物理删除前漏检文件变化", err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "new private copy" {
		t.Fatal("替换版本被误删", err)
	}
	next, err = restored.DiscardPreview("owner-a", batch.ID, id)
	if err != nil || next == revision {
		t.Fatal("无法重新确认新版本", err)
	}
	revision = next
	result, err := restored.DiscardDraft(ctx, "owner-a", batch.ID, id, revision, func(context.Context, string, string) error { return nil })
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != other {
		t.Fatal("恢复后清理范围错误", err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("原件未清")
	}
	if _, err = os.Stat(restored.tasks[batch.ID].Paths[other]); err != nil {
		t.Fatal("误删其他文件", err)
	}
}
