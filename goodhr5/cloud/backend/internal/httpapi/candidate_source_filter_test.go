// 文件作用说明：验证按插件平台查询多来源简历，兼容历史别名并隔离自动任务来源。
package httpapi

import (
	"strings"
	"testing"
)

// TestCandidatePluginSources 验证同一候选人的多个插件来源均可被查询，而自动任务不混入插件。
func TestCandidatePluginSources(t *testing.T) {
	item := PositionCandidate{PlatformID: "boss", LibraryFields: map[string]any{"source_type": "manual"}, Sources: []CandidateSource{{PlatformID: "hliepin", SourceType: "browser_extension"}, {PlatformID: "zhaopin", SourceType: "browser_extension"}, {PlatformID: "boss", SourceType: "automatic"}}}
	for _, source := range []string{"manual", "browser_extension", "browser_extension:liepin", "browser_extension:zhilian", "automatic"} {
		if !candidateMatchesSource(item, source) {
			t.Errorf("来源 %s 没有匹配多来源简历", source)
		}
	}
	for _, source := range []string{"browser_extension:boss", "browser_extension:51job", "browser_extension:unknown", "ai_import"} {
		if candidateMatchesSource(item, source) {
			t.Errorf("来源 %s 误匹配", source)
		}
	}
	legacy := PositionCandidate{PlatformID: "hliepin", LibraryFields: map[string]any{"source_type": "browser_extension"}}
	if !candidateMatchesSource(legacy, "browser_extension:liepin") {
		t.Fatal("历史猎聘别名无法查询")
	}
}

// TestCandidateSourceSQLParameters 验证数据库使用同样的多来源条件和安全参数，而不是字符串模糊匹配。
func TestCandidateSourceSQLParameters(t *testing.T) {
	args := []any{"tenant", "user"}
	condition := candidateSourceFilterSQL("browser_extension:liepin", &args)
	if !strings.Contains(condition, "candidate_profile_sources") || !strings.Contains(condition, "$3") || !strings.Contains(condition, "$7") || len(args) != 7 {
		t.Fatalf("来源查询条件错误: %s %#v", condition, args)
	}
	if condition := candidateSourceFilterSQL("browser_extension:boss' OR 1=1", &args); condition != "FALSE" {
		t.Fatal("未知来源不能放宽查询")
	}
}
