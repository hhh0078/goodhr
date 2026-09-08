// Package runtime 文件作用：验证运行组件压缩包只允许目录内的安全相对符号链接。
package runtime

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// TestExtractTarGZAllowsSafeRelativeSymlink 验证 Node 压缩包中的相对工具链接可以正常解压。
func TestExtractTarGZAllowsSafeRelativeSymlink(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "node.tar.gz")
	writeTarGZForTest(t, archivePath, []tar.Header{
		{Name: "node/bin/node", Mode: 0o755, Size: 4, Typeflag: tar.TypeReg},
		{Name: "node/bin/corepack", Linkname: "../lib/corepack.js", Typeflag: tar.TypeSymlink},
	})
	targetDir := t.TempDir()
	if err := extractTarGZ(archivePath, targetDir); err != nil {
		t.Fatal(err)
	}
	linkTarget, err := os.Readlink(filepath.Join(targetDir, "node", "bin", "corepack"))
	if err != nil || linkTarget != "../lib/corepack.js" {
		t.Fatalf("link target=%q err=%v", linkTarget, err)
	}
}

// TestExtractTarGZRejectsEscapingSymlink 验证越过组件目录的符号链接仍会被拒绝。
func TestExtractTarGZRejectsEscapingSymlink(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "unsafe.tar.gz")
	writeTarGZForTest(t, archivePath, []tar.Header{
		{Name: "node/bin/corepack", Linkname: "../../../outside", Typeflag: tar.TypeSymlink},
	})
	if err := extractTarGZ(archivePath, t.TempDir()); err == nil {
		t.Fatal("越界符号链接不应被解压")
	}
}

// writeTarGZForTest 创建只包含测试条目的 tar.gz 文件。
func writeTarGZForTest(t *testing.T, path string, headers []tar.Header) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for index := range headers {
		header := headers[index]
		if err = tarWriter.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err = tarWriter.Write([]byte("node")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}
