// Package replybridge 本文件验证服务器确认后只删除本次未被人工改动的临时文件。
package replybridge

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestAcknowledgementPreservesChangedDownload 验证云端收讫清理以摘要和修改时间为依据。
func TestAcknowledgementPreservesChangedDownload(t *testing.T) {
	for _, changed := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "resume.pdf")
		original := []byte("合成已上传内容")
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(path)
		hash := sha256.Sum256(original)
		e := &Executor{transfers: map[string][]transferredFile{"c": {{path: path, hash: hex.EncodeToString(hash[:]), modified: info.ModTime()}}}}
		if changed {
			if err := os.WriteFile(path, []byte("人工修改"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		e.Acknowledge("c")
		_, err := os.Stat(path)
		if changed && err != nil {
			t.Fatal("删除了人工修改文件")
		}
		if !changed && !os.IsNotExist(err) {
			t.Fatal("云端确认后的临时文件仍在")
		}
	}
}
