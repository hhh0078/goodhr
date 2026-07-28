// Package updater 文件作用：验证应用版本比较和更新包路径安全规则。
package updater

import "testing"

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
