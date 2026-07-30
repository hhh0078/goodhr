// Package greeting 文件作用：验证悬浮窗关键词结果与实际筛选规则保持一致。
package greeting

import (
	"reflect"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestMatchKeywordsReturnsStructuredHits 验证关键词和排除词命中结果会完整返回。
func TestMatchKeywordsReturnsStructuredHits(t *testing.T) {
	position := cloud.PositionSnapshot{
		Keywords: []string{"本科", "Go"}, ExcludeKeywords: []string{"外包"},
		IsAndMode: true,
	}
	candidate := model.Candidate{
		Name: "张三", Summary: "本科学历，5 年 Go 开发",
	}
	result := matchKeywords(candidate, "", position)
	if !result.Accepted || !reflect.DeepEqual(result.MatchedKeywords, []string{"本科", "Go"}) {
		t.Fatalf("关键词命中结果不正确：%+v", result)
	}
	rejected := matchKeywords(candidate, "曾参与外包项目", position)
	if rejected.Accepted || !reflect.DeepEqual(rejected.MatchedExcludes, []string{"外包"}) {
		t.Fatalf("排除词命中结果不正确：%+v", rejected)
	}
}
