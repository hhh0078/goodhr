// 本文件负责测试超管邮件自动任务的基础工具逻辑。
package httpapi

import (
	"testing"
	"time"
)

// TestNextRecoveryRun 验证自动挽回邮件会排到下一个目标小时。
func TestNextRecoveryRun(t *testing.T) {
	now := time.Date(2026, 7, 2, 8, 30, 0, 0, time.Local)
	sameDay := nextRecoveryRun(now, 9)
	if sameDay.Day() != 2 || sameDay.Hour() != 9 {
		t.Fatalf("sameDay = %v", sameDay)
	}
	nextDay := nextRecoveryRun(time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local), 9)
	if nextDay.Day() != 3 || nextDay.Hour() != 9 {
		t.Fatalf("nextDay = %v", nextDay)
	}
}
