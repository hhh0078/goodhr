// Package updater 文件作用：验证应用版本比较和更新包路径安全规则。
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompareVersion 验证版本比较兼容 v 前缀、补零和预发布后缀。
func TestCompareVersion(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "v1.2.0", right: "1.1.9", want: 1},
		{left: "1.2", right: "1.2.0", want: 0},
		{left: "1.2.0-beta", right: "1.2.1", want: -1},
	}
	for _, test := range tests {
		if got := compareVersion(test.left, test.right); got != test.want {
			t.Fatalf("compareVersion(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

// TestSafeArchivePathRejectsTraversal 验证更新压缩包不能越界写文件。
func TestSafeArchivePathRejectsTraversal(t *testing.T) {
	if _, err := safeArchivePath(t.TempDir(), "../outside.exe"); err == nil {
		t.Fatal("safeArchivePath() accepted traversal path")
	}
}

// TestUpdateDownloadRequiresHTTPSAndSHA256 验证程序更新拒绝明文地址和空校验值。
func TestUpdateDownloadRequiresHTTPSAndSHA256(t *testing.T) {
	if err := validateDownloadURL("https://oss.example.com/goodhr.pkg"); err != nil {
		t.Fatalf("HTTPS 更新地址被拒绝：%v", err)
	}
	if err := validateDownloadURL("http://oss.example.com/goodhr.pkg"); err == nil {
		t.Fatal("HTTP 更新地址不应被接受")
	}
	if err := validateSHA256(""); err == nil {
		t.Fatal("空 SHA256 不应被接受")
	}
}

// TestVerifySHA256 验证应用更新包内容必须匹配云端摘要。
func TestVerifySHA256(t *testing.T) {
	content := []byte("goodhr-update")
	path := filepath.Join(t.TempDir(), "goodhr.pkg")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := verifySHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("正确更新包校验失败：%v", err)
	}
	if err := verifySHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("错误更新包摘要不应通过")
	}
}
