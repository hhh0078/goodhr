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

// TestKeywordMatchUsesCompleteNormalizedDetail 验证只在详情中出现的关键词、OCR 断行和全角字符正确匹配。
func TestKeywordMatchUsesCompleteNormalizedDetail(t *testing.T) {
	position := cloud.PositionSnapshot{Keywords: []string{"本科", "Java"}, ExcludeKeywords: []string{"外包"}, IsAndMode: true}
	candidate := model.Candidate{Summary: "应聘软件工程师"}
	if matchKeywords(candidate, "", position).Accepted {
		t.Fatal("卡片没有关键词时不能假装命中")
	}
	if !matchKeywords(candidate, "学历：本\n科，技能：Ｊａ ｖａ", position).Accepted {
		t.Fatal("详情的 OCR 关键词未匹配")
	}
	if matchKeywords(candidate, "本科 Java，外\n包", position).Accepted {
		t.Fatal("详情排除词应优先拒绝")
	}
}
