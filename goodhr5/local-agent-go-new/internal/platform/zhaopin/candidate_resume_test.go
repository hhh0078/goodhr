// Package zhaopin 文件作用：验证推荐页的身份续跑和搜索页的原前缀保护互相隔离，禁止实际浏览器动作。
package zhaopin

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// resumeSearchBrowser 仅返回三张合成卡片，不提供点击、按键或滚轮能力。
type resumeSearchBrowser struct {
	model.Browser
	rows []map[string]string
}

// FindAll 返回按真实索引排列的身份字段，便于比较同一名单换序后的两种页面行为。
func (b *resumeSearchBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	var result []contract.FindAllItem
	for index := req.StartIndex; index < min(len(b.rows), req.StartIndex+req.MaxItems); index++ {
		result = append(result, contract.FindAllItem{Index: index, Fields: b.rows[index], Text: b.rows[index]["name"] + " 30岁"})
	}
	return result, nil
}

// TestSearchResumeRetainsPrefixProtection 验证同一换序名单在推荐页恢复，在搜索页仍从头；搜索分页、点击和筛选逻辑未被替换。
func TestSearchResumeRetainsPrefixProtection(t *testing.T) {
	data, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	browser := &resumeSearchBrowser{rows: []map[string]string{
		{"name": "合成甲", "basic_info": "30岁 本科", "education": "经历甲"},
		{"name": "合成乙", "basic_info": "30岁 本科", "education": "经历乙"},
		{"name": "合成丙", "basic_info": "30岁 本科", "education": "经历丙"},
	}}
	runtime := NewRuntime()
	runtime.manualSearch = true
	cursor := &model.CandidateListCursor{}
	_, _ = runtime.RestoreCandidateList(context.Background(), browser, cfg, model.CandidateResumePoint{}, cursor)
	rows, err := runtime.FindCandidates(context.Background(), browser, cfg, model.CandidateListOptions{Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	point, _ := runtime.CandidateResumePoint(rows[1])
	browser.rows[0], browser.rows[2] = browser.rows[2], browser.rows[0]
	if found, err := runtime.RestoreCandidateList(context.Background(), browser, cfg, point, cursor); err != nil || found || cursor.StartIndex != 0 {
		t.Fatalf("搜索页前缀保护变化：%v %v", found, err)
	}
	if found, err := NewRuntime().RestoreCandidateList(context.Background(), browser, cfg, point, cursor); err != nil || !found || cursor.StartIndex != 1 {
		t.Fatalf("推荐页没能按唯一身份恢复：%v %v", found, err)
	}
}
