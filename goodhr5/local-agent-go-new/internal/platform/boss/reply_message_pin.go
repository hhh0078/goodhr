// Package boss 本文件固定原消息的CSS行位置，再核对原文、总行数和三类原生身份，避免Worker过滤后再套用全局序号。
package boss

import (
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// pinBossMessageRow 在选择器阶段先固定原行，清除会在文本过滤后执行的Index，复用于附件同意及实际PDF预览。
func pinBossMessageRow(cfg model.Config, item contract.FindAllItem, text, candidateID string) (contract.SelectorSpec, error) {
	if item.TotalMatches <= 0 || item.TotalMatches > 5000 || item.Index < 0 || item.Index >= item.TotalMatches || strings.TrimSpace(text) == "" {
		return contract.SelectorSpec{}, fmt.Errorf("消息的原始位置和正文还没核实")
	}
	row, err := common.IndexedSelector(cfg, "message.item", item.Index)
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	if len(row.Parents) == 0 || len(row.Target.Selectors) != 1 || row.Target.Selectors[0].Type != "css" || len(row.Alternatives) != 0 {
		return contract.SelectorSpec{}, fmt.Errorf("消息行的页面结构还没核实")
	}
	row.Parents = append([]contract.SelectorGroup(nil), row.Parents...)
	if !bossNumericID(candidateID) || len(row.Parents[0].Selectors) != 1 || row.Parents[0].Selectors[0].Type != "css" {
		return contract.SelectorSpec{}, fmt.Errorf("消息所属聊天身份边界尚未核实")
	}
	rootCSS := row.Parents[0].Selectors[0].Value
	row.Parents[0].Selectors = append([]contract.SelectorCandidate(nil), row.Parents[0].Selectors...)
	for _, field := range []struct{ key, prefix string }{{"message.current_candidate_id", "get_resume_"}, {"message.current_phone_candidate_id", "get_phone_"}, {"message.current_wechat_candidate_id", "get_wechat_"}} {
		spec, found := cfg.Selectors[field.key]
		if !found || spec.ReadAttribute != "ka" || len(spec.Parents) == 0 || len(spec.Parents[0].Selectors) != 1 || spec.Parents[0].Selectors[0].Type != "css" || spec.Parents[0].Selectors[0].Value != rootCSS || len(spec.Target.Selectors) != 1 || spec.Target.Selectors[0].Type != "css" {
			return contract.SelectorSpec{}, fmt.Errorf("消息的原生身份定位尚未核实")
		}
		row.Parents[0].Selectors[0].Value += fmt.Sprintf(":has(%s[ka=%q])", spec.Target.Selectors[0].Value, field.prefix+candidateID)
	}
	parent := &row.Parents[len(row.Parents)-1]
	if len(parent.Selectors) != 1 || parent.Selectors[0].Type != "css" {
		return contract.SelectorSpec{}, fmt.Errorf("消息列表结构还没核实")
	}
	parent.Selectors = append([]contract.SelectorCandidate(nil), parent.Selectors...)
	css := row.Target.Selectors[0].Value
	parent.Selectors[0].Value += fmt.Sprintf(":has(> %s:nth-child(%d)):not(:has(> %s:nth-child(%d)))", css, item.TotalMatches, css, item.TotalMatches+1)
	row.Target.Selectors = append([]contract.SelectorCandidate(nil), row.Target.Selectors...)
	row.Target.Selectors[0].Value = fmt.Sprintf(":nth-match(%s, %d)", css, item.Index+1)
	row.Target.Index = nil
	// innerText会按块元素插入换行，而hasText依据DOM文本匹配；逐段保留全部原文约束，避免把布局换行当作正文字符。
	row.Target.Texts = append(append([]string(nil), row.Target.Texts...), strings.Fields(text)...)
	return row, nil
}
