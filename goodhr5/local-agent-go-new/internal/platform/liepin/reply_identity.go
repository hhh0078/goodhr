// Package liepin 本文件通过当前可见聊天框的标准读取能力核验精确候选人编号，供单会话测试限制使用。
package liepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// VerifyAutoReplyConversationIdentity 只读核对当前姓名和按钮cid，不从历史消息或旧快照推测当前页面归属。
func (r *Runtime) VerifyAutoReplyConversationIdentity(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if strings.TrimSpace(snapshot.CandidateName) == "" || strings.TrimSpace(snapshot.PlatformCandidateID) == "" {
		return fmt.Errorf("指定候选人的姓名或平台编号不完整，已阻止页面动作")
	}
	// 缺少配置不能当成聊天关闭，只有 Worker 明确找不到姓名节点才允许流程重新打开目标。
	if _, err := common.RequiredSelector(cfg, "message.candidate_id_source"); err != nil {
		return err
	}
	name, found, err := readLiepinCurrentConversationName(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if !found {
		return model.ErrAutoReplyConversationNotOpen
	}
	if strings.TrimSpace(name) != snapshot.CandidateName {
		return fmt.Errorf("当前聊天框不是指定候选人，已阻止页面动作")
	}
	actualID, err := readLiepinCandidateID(ctx, browser, cfg, nil)
	if err != nil {
		return err
	}
	if actualID == "" || actualID != snapshot.PlatformCandidateID {
		return fmt.Errorf("当前聊天框的候选人编号无法核实，已阻止页面动作")
	}
	return nil
}

// readLiepinCurrentConversationName 先短探测当前原页的聊天姓名，缺失时不执行配置中三秒等待的读取，节点消失的竞态也仅用短预算。
func readLiepinCurrentConversationName(ctx context.Context, browser model.Browser, cfg model.Config) (string, bool, error) {
	selector, err := common.RequiredSelector(cfg, "message.current_name")
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(cfg.MessagesURL) != "" {
		page, err := common.UniqueCurrentPageURL(ctx, browser)
		if err != nil {
			return "", false, err
		}
		if !common.PageURLMatches(page, cfg.MessagesURL) {
			return "", false, fmt.Errorf("当前页面与招聘会话入口不符，暂不核对聊天")
		}
	}
	// 只复制本次只读配置，不能把临时预算改回共享平台配置。
	readCfg := cfg
	readCfg.Selectors = map[string]contract.SelectorSpec{"message.current_name": selector.MapVariants(func(spec contract.SelectorSpec) contract.SelectorSpec {
		if spec.TimeoutMS <= 0 || spec.TimeoutMS > 600 {
			spec.TimeoutMS = 600
		}
		return spec
	})}
	found, err := common.ProbeSelectorExists(ctx, browser, readCfg, "message.current_name")
	if err != nil || !found {
		return "", false, err
	}
	name, found, err := common.ReadOptional(ctx, browser, readCfg, "message.current_name")
	if err == nil && found && strings.TrimSpace(name) == "" {
		return "", false, fmt.Errorf("当前聊天姓名为空，暂不猜测候选人")
	}
	return name, found, err
}

// openLiepinConversation 优先按稳定会话属性点击，避免同名或列表重排点错人；无平台属性的普通会话兼容旧入口。
func openLiepinConversation(ctx context.Context, browser model.Browser, cfg model.Config, expected model.Conversation) error {
	attribute := cfg.ConversationFields["thread_meta"].ReadAttribute
	if strings.TrimSpace(expected.PlatformThreadID) == "" || attribute == "" {
		if expected.PlatformCandidateID != "" {
			return fmt.Errorf("指定会话的稳定页面属性无法确认，已阻止打开")
		}
		index, err := locateLiepinConversation(ctx, browser, cfg, expected)
		if err != nil {
			return err
		}
		return common.OpenConfiguredConversationItem(ctx, browser, cfg, index)
	}
	card, err := common.RequiredSelector(cfg, "message.contact_item")
	if err != nil {
		return err
	}
	metadata := ""
	if name := strings.TrimSpace(expected.Name); name != "" {
		scoped := card.MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
			variant.Target.Texts = append(append([]string(nil), variant.Target.Texts...), name)
			return variant
		})
		metadata, err = findLiepinContactMetadata(ctx, browser, cfg, scoped, expected.PlatformThreadID, name)
		if err != nil {
			return err
		}
		if metadata != "" {
			card = scoped
		}
	}
	if metadata == "" {
		// 普通会话允许候选人改名，通过全表的会话属性找回；限定任务还必须核对姓名。
		name := ""
		if expected.PlatformCandidateID != "" {
			name = strings.TrimSpace(expected.Name)
			if name == "" {
				return fmt.Errorf("指定会话缺少候选人姓名，已阻止打开")
			}
		}
		metadata, err = findLiepinContactMetadata(ctx, browser, cfg, card, expected.PlatformThreadID, name)
		if err != nil {
			return err
		}
		if metadata == "" {
			return fmt.Errorf("指定会话的稳定页面属性无法确认，已阻止打开")
		}
	}
	card = card.MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
		attributes := make(map[string]string, len(variant.Target.Attributes)+1)
		for key, value := range variant.Target.Attributes {
			attributes[key] = value
		}
		attributes[attribute] = metadata
		variant.Target.Attributes = attributes
		variant.Target.Index = nil
		return variant
	})
	action, err := common.RequiredSelector(cfg, "message.contact_click_target")
	if err != nil {
		return err
	}
	action = action.WithinSelector(card)
	anchor, err := common.RequiredSelector(cfg, "message.drawer_scroll")
	if err != nil {
		return err
	}
	requireFull := true
	_, err = browser.Scroll(ctx, contract.ScrollRequest{Target: &action, WheelAnchor: &anchor, Distance: 180, MaxAttempts: 18, WaitMS: 180, RequireFull: &requireFull, ViewportMargin: 12})
	if err != nil {
		return fmt.Errorf("滚动到指定会话失败，已阻止打开：%w", err)
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: action, ViewportMargin: 0})
	return err
}

// findLiepinContactMetadata 从当前渲染的联系人行读取会话编号；要求姓名时再读取姓名，任何歧义都阻止点击。
func findLiepinContactMetadata(ctx context.Context, browser model.Browser, cfg model.Config, selector contract.SelectorSpec, threadID, name string) (string, error) {
	fields := map[string]contract.SelectorSpec{"thread_meta": cfg.ConversationFields["thread_meta"]}
	if field, ok := cfg.ConversationFields["name"]; name != "" && ok {
		fields["name"] = field
	}
	const maxContacts = 5000 // Worker 单次可核验的最大列表长度。
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: maxContacts, Fields: fields, ExpectedMissing: true})
	if common.IsElementMissing(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	match := ""
	for _, item := range items {
		if item.TotalMatches > maxContacts {
			return "", fmt.Errorf("联系人数量超过单次核验范围，无法确认唯一会话")
		}
		meta, err := parseLiepinContactMeta(item.Fields["thread_meta"])
		if err != nil {
			return "", fmt.Errorf("解析猎聘联系人会话编号失败：%w", err)
		}
		if strings.TrimSpace(meta.ThreadID) != strings.TrimSpace(threadID) || (name != "" && strings.TrimSpace(item.Fields["name"]) != name) {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("匹配到多个相同会话编号的联系人，已阻止打开")
		}
		match = item.Fields["thread_meta"]
	}
	return match, nil
}
