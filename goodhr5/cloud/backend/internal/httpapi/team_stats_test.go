// 本文件负责团队统计接口的基础日期周期测试。
package httpapi

import (
	"testing"
	"time"
)

// TestResolveTeamStatsRange 校验团队统计默认本月和自定义周期。
func TestResolveTeamStatsRange(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, loc)
	start, end, period := resolveTeamStatsRange("", "", "", now)
	if period != "month" || start.Format(time.DateOnly) != "2026-07-01" || end.Format(time.DateOnly) != "2026-08-01" {
		t.Fatalf("month range = %s %s %s", period, start.Format(time.DateOnly), end.Format(time.DateOnly))
	}
	start, end, period = resolveTeamStatsRange("custom", "2026-07-03", "2026-07-08", now)
	if period != "custom" || start.Format(time.DateOnly) != "2026-07-03" || end.Format(time.DateOnly) != "2026-07-09" {
		t.Fatalf("custom range = %s %s %s", period, start.Format(time.DateOnly), end.Format(time.DateOnly))
	}
}
