// Package logfile 文件作用：验证本地进程日志按大小轮转并限制历史文件数量。
package logfile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriterRotatesAndKeepsLimitedBackups 验证当前日志超限后轮转且只保留指定备份数。
func TestWriterRotatesAndKeepsLimitedBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	writer, err := Open(path, 8, 2)
	if err != nil {
		t.Fatalf("打开轮转日志失败：%v", err)
	}
	for _, content := range []string{"12345678", "abcdefgh", "ABCDEFGH", "87654321"} {
		if _, err = writer.Write([]byte(content)); err != nil {
			t.Fatalf("写入轮转日志失败：%v", err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("关闭轮转日志失败：%v", err)
	}
	for _, expectedPath := range []string{path, path + ".1", path + ".2"} {
		if _, err = os.Stat(expectedPath); err != nil {
			t.Fatalf("缺少预期日志文件 %s：%v", expectedPath, err)
		}
	}
	if _, err = os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("日志备份数量超过上限：%v", err)
	}
}
