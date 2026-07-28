// Package common 实现由云端 URL 和统一选择器配置驱动的跨平台页面操作。
package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// Runtime 实现平台共用的配置驱动页面动作。
type Runtime struct {
	platformID string
}

// New 创建指定平台编号的通用运行时。
func New(platformID string) *Runtime {
	return &Runtime{platformID: platformID}
}

// PrepareGreeting 打开入口页并按配置准备岗位和筛选。
func (r *Runtime) PrepareGreeting(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	if strings.TrimSpace(cfg.EntryURL) == "" {
		return fmt.Errorf("平台 %s 没有配置入口地址", r.platformID)
	}
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: cfg.EntryURL, WaitUntil: "domcontentloaded", TimeoutMS: 30000}); err != nil {
		return err
	}
	steps := []struct {
		name     string
		required bool
		run      func() error
	}{
		{name: "关闭入口提示", run: func() error { return clickOptional(ctx, browser, cfg, "entry.dismiss") }},
		{name: "打开岗位选择", required: true, run: func() error { return clickOptional(ctx, browser, cfg, "position.open") }},
		{name: "选择岗位", required: true, run: func() error { return selectPosition(ctx, browser, cfg, position.Name) }},
		{name: "应用筛选", required: true, run: func() error { return clickOptional(ctx, browser, cfg, "filter.apply") }},
	}
	for _, step := range steps {
		if err := step.run(); err != nil && step.required {
			return fmt.Errorf("%s失败：%w", step.name, err)
		}
	}
	return nil
}

// ScanCandidates 读取当前可见候选人和配置字段。
func (r *Runtime) ScanCandidates(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Candidate, error) {
	selector, err := requiredSelector(cfg, "candidate.item")
	if err != nil {
		return nil, err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector,
		MaxItems: positiveOr(cfg.MaxItems, 100),
		Fields:   cfg.CandidateFields,
	})
	if err != nil {
		return nil, err
	}
	candidates := make([]model.Candidate, 0, len(items))
	for _, item := range items {
		name := firstNonEmpty(item.Fields["name"], item.Text)
		fingerprint := firstNonEmpty(item.Fields["id"], hashText(r.platformID+"|"+name+"|"+item.Text))
		candidates = append(candidates, model.Candidate{
			Index:       item.Index,
			Fingerprint: fingerprint,
			Name:        name,
			Summary:     item.Text,
			Fields:      item.Fields,
		})
	}
	return candidates, nil
}

// ReadCandidateDetail 打开候选人并读取详情文本。
func (r *Runtime) ReadCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (model.CandidateDetail, error) {
	item, err := candidateScopedSelector(cfg, "candidate.open_target", candidate.Index)
	if err != nil {
		return model.CandidateDetail{}, err
	}
	if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: item}); err != nil {
		fallback, fallbackErr := indexedSelector(cfg, "candidate.item", candidate.Index)
		if fallbackErr != nil {
			return model.CandidateDetail{}, err
		}
		if _, fallbackErr = browser.Click(ctx, contract.ElementClickRequest{Selector: fallback}); fallbackErr != nil {
			return model.CandidateDetail{}, fallbackErr
		}
	}
	detail, err := requiredSelector(cfg, "candidate.detail")
	if err != nil {
		return model.CandidateDetail{}, err
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: detail, Property: "text"})
	if err != nil {
		return model.CandidateDetail{}, err
	}
	return model.CandidateDetail{Text: result.Value}, nil
}

// GreetCandidate 点击候选人打招呼入口并按配置发送可选自定义文案。
func (r *Runtime) GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, message string) error {
	if strings.TrimSpace(message) != "" {
		if err := inputOptional(ctx, browser, cfg, "candidate.greet_input", message); err != nil {
			return err
		}
	}
	selector, err := candidateScopedSelector(cfg, "candidate.greet_send", candidate.Index)
	if err != nil {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
	return err
}

// CloseCandidateDetail 关闭当前候选人详情，可选选择器未配置时不执行。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return clickOptional(ctx, browser, cfg, "candidate.detail_close")
}

// ScrollCandidates 使用真实鼠标滚轮继续加载候选人。
func (r *Runtime) ScrollCandidates(ctx context.Context, browser model.Browser, cfg model.Config) error {
	var target *contract.SelectorSpec
	if selector, ok := cfg.Selectors["candidate.list"]; ok {
		target = &selector
	}
	_, err := browser.Scroll(ctx, contract.ScrollRequest{
		Target:      target,
		Distance:    positiveOr(cfg.ScrollDistance, 620),
		MaxAttempts: 1,
		WaitMS:      350,
	})
	return err
}

// PrepareAutoReply 打开平台消息页并关闭可选提示。
func (r *Runtime) PrepareAutoReply(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if strings.TrimSpace(cfg.MessagesURL) == "" {
		return fmt.Errorf("平台 %s 没有配置消息页地址", r.platformID)
	}
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: cfg.MessagesURL, WaitUntil: "domcontentloaded", TimeoutMS: 30000}); err != nil {
		return err
	}
	return clickOptional(ctx, browser, cfg, "message.dismiss")
}

// ScanUnreadConversations 读取未读会话列表和配置字段。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	selector, err := requiredSelector(cfg, "message.unread_item")
	if err != nil {
		return nil, err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector,
		MaxItems: positiveOr(cfg.MaxItems, 100),
		Fields:   cfg.ConversationFields,
	})
	if err != nil {
		return nil, err
	}
	conversations := make([]model.Conversation, 0, len(items))
	for _, item := range items {
		name := firstNonEmpty(item.Fields["name"], item.Text)
		key := firstNonEmpty(item.Fields["id"], hashText(r.platformID+"|"+name+"|"+item.Text))
		conversations = append(conversations, model.Conversation{
			Index: item.Index, Key: key, Name: name, Summary: item.Text, Fields: item.Fields,
		})
	}
	return conversations, nil
}

// ReadConversation 打开未读会话并读取上下文。
func (r *Runtime) ReadConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (string, error) {
	item, err := indexedSelector(cfg, "message.unread_item", conversation.Index)
	if err != nil {
		return "", err
	}
	if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: item}); err != nil {
		return "", err
	}
	contextSelector, err := requiredSelector(cfg, "message.context")
	if err != nil {
		return "", err
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: contextSelector, Property: "text"})
	return result.Value, err
}

// ReplyConversation 输入回复并点击发送。
func (r *Runtime) ReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, reply string) error {
	if strings.TrimSpace(reply) == "" {
		return fmt.Errorf("回复内容不能为空")
	}
	if err := inputRequired(ctx, browser, cfg, "message.input", reply); err != nil {
		return err
	}
	return clickRequired(ctx, browser, cfg, "message.send")
}

// requiredSelector 返回平台必需选择器。
func requiredSelector(cfg model.Config, key string) (contract.SelectorSpec, error) {
	selector, ok := cfg.Selectors[key]
	if !ok || len(selector.Target.Selectors) == 0 {
		return contract.SelectorSpec{}, fmt.Errorf("平台 %s 缺少选择器 %s", cfg.ID, key)
	}
	return selector, nil
}

// indexedSelector 复制选择器并设置从 0 开始的列表序号。
func indexedSelector(cfg model.Config, key string, index int) (contract.SelectorSpec, error) {
	selector, err := requiredSelector(cfg, key)
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	indexValue := max(index, 0)
	selector.Target.Index = &indexValue
	return selector, nil
}

// candidateScopedSelector 把卡片序号作为父级，并在卡片内部定位具体动作。
func candidateScopedSelector(cfg model.Config, actionKey string, index int) (contract.SelectorSpec, error) {
	card, err := requiredSelector(cfg, "candidate.item")
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	action, ok := cfg.Selectors[actionKey]
	if !ok {
		return indexedSelector(cfg, "candidate.item", index)
	}
	indexValue := max(index, 0)
	card.Target.Index = &indexValue
	parents := make([]contract.SelectorGroup, 0, len(card.Parents)+1+len(action.Parents))
	parents = append(parents, card.Parents...)
	parents = append(parents, card.Target)
	parents = append(parents, action.Parents...)
	action.Parents = parents
	action.Frames = append(card.Frames, action.Frames...)
	action.Description = actionKey
	return action, nil
}

// selectPosition 按输入框或岗位文本选择云端岗位名称。
func selectPosition(ctx context.Context, browser model.Browser, cfg model.Config, positionName string) error {
	if _, ok := cfg.Selectors["position.input"]; ok {
		if err := inputRequired(ctx, browser, cfg, "position.input", positionName); err != nil {
			return err
		}
	}
	selector, ok := cfg.Selectors["position.item"]
	if !ok {
		return nil
	}
	selector.Target.Text = positionName
	selector.Target.ExactText = boolPointer(false)
	_, err := browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
	return err
}

// clickRequired 调用 Worker 完整点击能力。
func clickRequired(ctx context.Context, browser model.Browser, cfg model.Config, key string) error {
	selector, err := requiredSelector(cfg, key)
	if err != nil {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
	return err
}

// clickOptional 点击存在于云端配置中的可选元素。
func clickOptional(ctx context.Context, browser model.Browser, cfg model.Config, key string) error {
	selector, ok := cfg.Selectors[key]
	if !ok {
		return nil
	}
	_, err := browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
	return err
}

// inputRequired 调用 Worker 完整输入能力。
func inputRequired(ctx context.Context, browser model.Browser, cfg model.Config, key string, value string) error {
	selector, err := requiredSelector(cfg, key)
	if err != nil {
		return err
	}
	clear := true
	verify := true
	_, err = browser.Input(ctx, contract.ElementInputRequest{Selector: selector, Text: value, Clear: &clear, Verify: &verify})
	return err
}

// inputOptional 向存在于云端配置中的可选元素输入内容。
func inputOptional(ctx context.Context, browser model.Browser, cfg model.Config, key string, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if _, ok := cfg.Selectors[key]; !ok {
		return nil
	}
	return inputRequired(ctx, browser, cfg, key, value)
}

// positiveOr 返回正整数配置或默认值。
func positiveOr(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// hashText 返回用于本地去重的短哈希。
func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// boolPointer 返回布尔值指针供可选协议字段使用。
func boolPointer(value bool) *bool {
	return &value
}
