// Package resume_download 本文件验证物理删除严格受Agent私有根、下载记录、当前摘要与所有引用保护。
package resume_download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"goodhr5/local-agent-go-new/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

// TestPrivateDownloadPurgeProtectsUserOriginalsAndSharedReferences 验证用户下载、共享路径、改名覆盖和无记录文件均不被误删。
func TestPrivateDownloadPurgeProtectsUserOriginalsAndSharedReferences(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	personal := t.TempDir()
	original := []byte("fixture resume")
	h := sha256.Sum256(original)
	digest := hex.EncodeToString(h[:])
	removed := map[string]entry{}
	m := Manager{entries: map[string]entry{}}
	var rows []storage.DownloadRecord
	for _, name := range []string{"private", "personal", "shared", "changed", "untracked"} {
		dir := root
		if name == "personal" {
			dir = personal
		}
		path := filepath.Join(dir, name+".pdf")
		content := original
		if name == "changed" {
			content = []byte("fresh replacement")
		}
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		removed[name] = entry{ID: name, Path: path, Hash: digest, Platform: "liepin", Owner: "mine"}
		if name != "untracked" {
			rows = append(rows, storage.DownloadRecord{ID: name, FilePath: path, Status: "saved"})
		}
		if name == "shared" {
			rows = append(rows, storage.DownloadRecord{ID: "someone-else", FilePath: path, Status: "saved"})
		}
	}
	m.SetFilePurge(func(context.Context) ([]storage.DownloadRecord, error) { return rows, nil }, func(path string) bool { return filepath.Dir(path) == root })
	if err := m.removeVerifiedFiles(ctx, removed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(removed["private"].Path); !os.IsNotExist(err) {
		t.Fatal("私有独占副本未移除")
	}
	for _, name := range []string{"personal", "shared", "changed", "untracked"} {
		b, err := os.ReadFile(removed[name].Path)
		if err != nil {
			t.Fatalf("%s 受误删: %v", name, err)
		}
		sum := sha256.Sum256(b)
		want := h
		if name == "changed" {
			want = sha256.Sum256([]byte("fresh replacement"))
		}
		if sum != want {
			t.Fatalf("%s 被改写", name)
		}
	}
}
