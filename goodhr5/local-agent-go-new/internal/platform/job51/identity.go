// Package job51 文件作用：解析前程无忧卡片编号的索引尾缀，并在名单换序后按稳定简历编号重新定位唯一候选人。
package job51

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var errCandidateCardMissing = errors.New("前程无忧当前名单中没有找到目标候选人")

type identityBrowser struct{ model.Browser }

// FindAll 只在候选人字段读取时去掉DOM编号的真实列表索引尾缀，供本平台读取和公共滚动比较复用。
func (b identityBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	items, err := b.Browser.FindAll(ctx, request)
	if err != nil {
		return nil, err
	}
	if _, requested := request.Fields["platform_candidate_id"]; !requested {
		return items, nil
	}
	trace := common.CandidateTrace(ctx)
	if trace != nil {
		n := len(items)
		trace.Facts.CardsRead = &n
		trace.RecordMatches(items)
		trace.Done()
		trace.Begin("核对卡片身份编号与列表行号", "candidate.item")
	}
	items = append([]contract.FindAllItem(nil), items...)
	for index := range items {
		raw := strings.TrimSpace(items[index].Fields["platform_candidate_id"])
		stable, err := stableCardID(raw, items[index].Index)
		if err != nil {
			return nil, err
		}
		fields := make(map[string]string, len(items[index].Fields)+1)
		for key, value := range items[index].Fields {
			fields[key] = value
		}
		fields["card_dom_id"] = raw
		fields["platform_candidate_id"] = stable
		items[index].Fields = fields
	}
	return items, nil
}

// stableCardID 按已验证格式去掉前缀和完整十进制索引；尾缀不符、编号非数字或为空时拒绝猜测。
func stableCardID(raw string, index int) (string, error) {
	const prefix = "RESUME_CARD_"
	suffix := strconv.Itoa(index)
	if index < 0 || !strings.HasPrefix(raw, prefix) || !strings.HasSuffix(raw, suffix) {
		return "", fmt.Errorf("前程无忧卡片身份编号和列表位置没对上，这批先不操作，请等名单加载好后重试")
	}
	value := strings.TrimSuffix(strings.TrimPrefix(raw, prefix), suffix)
	if !numericCandidateID(value) {
		return "", fmt.Errorf("前程无忧卡片身份编号还没读完整，这批先不操作，请重新读取名单")
	}
	return value, nil
}

// numericCandidateID 只接受非空的十进制简历编号，不使用会溢出的整型转换。
func numericCandidateID(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// scopeCandidate 通过标准HTML读取当前主列表，在Go中解析公开卡片编号，唯一匹配后给原公共动作附加精确DOM编号。
func scopeCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (model.Config, model.Candidate, error) {
	stable := strings.TrimSpace(candidate.Fields["platform_candidate_id"])
	if !numericCandidateID(stable) {
		return cfg, candidate, fmt.Errorf("前程无忧候选人身份还没确认，这次先不点击，请重新读取名单")
	}
	list, err := common.RequiredSelector(cfg, "candidate.list")
	if err != nil {
		return cfg, candidate, err
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: list, Property: "html"})
	if err != nil {
		return cfg, candidate, err
	}
	doc, err := html.Parse(strings.NewReader(result.Value))
	if err != nil {
		return cfg, candidate, fmt.Errorf("读取前程无忧候选人卡片编号失败：%w", err)
	}
	ids := cardDOMIDs(doc)
	matched := ""
	matchedIndex := -1
	rawCounts := make(map[string]int, len(ids))
	for index, raw := range ids {
		rawCounts[raw]++
		id, err := stableCardID(raw, index)
		if err != nil {
			return cfg, candidate, err
		}
		if id != stable {
			continue
		}
		if matched != "" {
			return cfg, candidate, fmt.Errorf("前程无忧名单里出现多个相同身份编号，这次先不点击，避免选错人")
		}
		matched = raw
		matchedIndex = index
	}
	if matched == "" {
		return cfg, candidate, errCandidateCardMissing
	}
	if rawCounts[matched] != 1 {
		return cfg, candidate, fmt.Errorf("前程无忧卡片定位编号重复，这次先不点击，请刷新名单后重试")
	}
	card, err := common.RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return cfg, candidate, err
	}
	card = card.MapVariants(func(spec contract.SelectorSpec) contract.SelectorSpec {
		attributes := make(map[string]string, len(spec.Target.Attributes)+1)
		for key, value := range spec.Target.Attributes {
			attributes[key] = value
		}
		attributes["id"] = matched
		spec.Target.Attributes = attributes
		spec.Target.Index = nil
		return spec
	})
	selectors := make(map[string]contract.SelectorSpec, len(cfg.Selectors))
	for key, value := range cfg.Selectors {
		selectors[key] = value
	}
	selectors["candidate.item"] = card
	cfg.Selectors = selectors
	fields := make(map[string]string, len(candidate.Fields)+1)
	for key, value := range candidate.Fields {
		fields[key] = value
	}
	fields["card_dom_id"] = matched
	candidate.Fields = fields
	// Worker先取序号、后核验Attributes；必须使用当前主列表真实序号，再用精确编号防止换序串人。
	candidate.Index = matchedIndex
	candidate.IdentityOccurrence = 0
	candidate.IdentityTexts = nil
	return cfg, candidate, nil
}

// cardDOMIDs 按当前主列表的DOM顺序收集li公开编号，不读取脚本状态，也不把编号加入可见文本。
func cardDOMIDs(node *html.Node) []string {
	ids := make([]string, 0)
	if node.Type == html.ElementNode && node.Data == "li" {
		for _, attr := range node.Attr {
			if attr.Key == "id" && strings.HasPrefix(attr.Val, "RESUME_CARD_") {
				ids = append(ids, attr.Val)
				break
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		ids = append(ids, cardDOMIDs(child)...)
	}
	return ids
}
