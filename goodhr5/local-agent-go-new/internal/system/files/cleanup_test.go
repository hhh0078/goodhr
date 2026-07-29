// Package files 文件作用：验证 GoodHR 临时文件只按修改时间清理过期内容。
package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRemoveOlderThan 验证过期文件删除、近期文件保留。
func TestRemoveOlderThan(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.tmp")
	newPath := filepath.Join(root, "new.tmp")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	deleted, err := RemoveOlderThan(root, time.Now().Add(-24*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("清理结果不正确：deleted=%d err=%v", deleted, err)
	}
	if _, err = os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("过期文件没有删除")
	}
	if _, err = os.Stat(newPath); err != nil {
		t.Fatalf("近期文件被误删：%v", err)
	}
}
