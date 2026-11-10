// Package boss 本文件从当前可见会话核对稳定身份，并按联系人原生编号恢复原会话。
package boss

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var _ model.AutoReplyIdentityVerifier = (*Runtime)(nil)
var _ model.AutoReplyContactInspector = (*Runtime)(nil)

// bossAutoReplyPageURL 只接受当前唯一活动的BOSS会话入口，不替换或重新打开招聘标签页。
func bossAutoReplyPageURL(ctx context.Context, browser model.Browser, cfg model.Config) (string, error) {
	page, err := common.UniqueCurrentPageURL(ctx, browser)
	if err != nil {
		return "", err
	}
	if !common.PageURLMatches(page, cfg.MessagesURL) {
		return "", fmt.Errorf("当前页面不是BOSS会话入口，请先回到推荐页")
	}
	return page, nil
}

// ensureBossConversationDrawer 复用全部联系人列表；收起时一次打开并验证，不依赖未读角标。
func ensureBossConversationDrawer(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if _, err := bossAutoReplyPageURL(ctx, browser, cfg); err != nil {
		return err
	}
	opened, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
	if err != nil || opened {
		return err
	}
	entry, err := common.RequiredSelector(cfg, "message.entry")
	if err != nil {
		return err
	}
	drawer, err := common.RequiredSelector(cfg, "message.drawer")
	if err != nil {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: entry, Verify: &contract.ClickVerification{TargetVisible: &drawer, TimeoutMS: 1800}})
	return err
}

// VerifyAutoReplyConversationIdentity 逐次读取当前候选人编号和姓名；同名、缺编号及换页均阻止动作。
func (r *Runtime) VerifyAutoReplyConversationIdentity(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if _, err := common.RequiredSelector(cfg, "message.current_name"); err != nil {
		return err
	}
	name, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_name")
	if err != nil {
		return err
	}
	if !found {
		return model.ErrAutoReplyConversationNotOpen
	}
	return verifyBossIdentityFields(ctx, browser, cfg, snapshot, name)
}

// verifyBossIdentityFields 复用本轮已读取的姓名，避免同一个身份核验再次读取相同节点。
func verifyBossIdentityFields(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, name string) error {
	if snapshot.CandidateName == "" || snapshot.PlatformCandidateID == "" || snapshot.PlatformThreadID == "" {
		return fmt.Errorf("指定BOSS会话的姓名或稳定编号不完整，暂不操作")
	}
	page, err := bossAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if snapshot.DeliveryPageURL != "" && page != snapshot.DeliveryPageURL {
		return fmt.Errorf("消息发送期间BOSS页面已变化，暂不操作")
	}
	if strings.TrimSpace(name) != strings.TrimSpace(snapshot.CandidateName) {
		return fmt.Errorf("当前BOSS聊天不是指定候选人，暂不操作")
	}
	candidateID, err := readBossCurrentCandidateID(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if candidateID == "" || candidateID != snapshot.PlatformCandidateID {
		return fmt.Errorf("当前BOSS候选人编号无法核实，暂不操作")
	}
	if _, configured := cfg.Selectors["message.current_thread_id"]; configured {
		threadID, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_thread_id")
		if err != nil {
			return err
		}
		if (!found || threadID == "") && strings.HasPrefix(snapshot.PlatformThreadID, "local:") {
			return nil // 无key置顶行已由唯一可见联系人与三个原生按钮编号核实，路由键明确标为本地键。
		}
		if !found || threadID == "" || threadID != snapshot.PlatformThreadID {
			return fmt.Errorf("当前BOSS会话编号与指定会话不符，暂不操作")
		}
	}
	return nil
}

// bossAutoReplyConversationMatches 只核对当前聊天，不会为发送后的回执恢复或切换会话。
func bossAutoReplyConversationMatches(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (bool, error) {
	if _, err := common.RequiredSelector(cfg, "message.current_name"); err != nil {
		return false, err
	}
	name, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_name")
	if err != nil || !found {
		return false, err
	}
	if strings.TrimSpace(name) != strings.TrimSpace(snapshot.CandidateName) {
		return false, fmt.Errorf("BOSS聊天已切换到其他候选人，暂不继续")
	}
	if err = verifyBossIdentityFields(ctx, browser, cfg, snapshot, name); err != nil {
		return false, err
	}
	if snapshot.CommunicationPosition != "" {
		position, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_position")
		if err != nil {
			return false, err
		}
		if !found || cleanBossAutoReplyPosition(position) != cleanBossAutoReplyPosition(snapshot.CommunicationPosition) {
			return false, fmt.Errorf("BOSS聊天沟通岗位已变化，暂不继续")
		}
	}
	return true, nil
}

// ensureBossAutoReplyConversation 仅在原聊天明确关闭时恢复；发送后的页面与回执检查始终留在原页。
func ensureBossAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	matched, err := bossAutoReplyConversationMatches(ctx, browser, cfg, snapshot)
	if err != nil || matched {
		return err
	}
	if snapshot.DeliveryPageURL != "" {
		return model.ErrAutoReplyConversationNotOpen
	}
	item := snapshot.Conversation
	item.Name = snapshot.CandidateName
	item.PlatformThreadID = snapshot.PlatformThreadID
	item.PlatformCandidateID = snapshot.PlatformCandidateID
	item.CommunicationPosition = snapshot.CommunicationPosition
	_, err = openBossAutoReplyContact(ctx, browser, cfg, item)
	return err
}

// InspectAutoReplyContact 只读会话头，不读取历史或旧附件，供删除后的新沟通核身使用。
func (r *Runtime) InspectAutoReplyContact(ctx context.Context, browser model.Browser, cfg model.Config, item model.Conversation) (model.AutoReplyConversationSnapshot, error) {
	return openBossAutoReplyContact(ctx, browser, cfg, item)
}

// openBossAutoReplyContact 先用原生会话属性定位唯一联系人，再核对姓名、候选编号和岗位。
func openBossAutoReplyContact(ctx context.Context, browser model.Browser, cfg model.Config, item model.Conversation) (model.AutoReplyConversationSnapshot, error) {
	var snapshot model.AutoReplyConversationSnapshot
	if item.PlatformThreadID == "" || item.Name == "" {
		return snapshot, fmt.Errorf("BOSS联系人缺少稳定会话编号或姓名，暂不打开")
	}
	if err := ensureBossConversationDrawer(ctx, browser, cfg); err != nil {
		return snapshot, err
	}
	row, err := common.RequiredSelector(cfg, "message.contact_item")
	if err != nil {
		return snapshot, err
	}
	thread, ok := cfg.ConversationFields["thread_id"]
	if !ok || thread.ReadAttribute == "" {
		return snapshot, fmt.Errorf("BOSS联系人原生编号属性未配置，暂不打开")
	}
	localKey := strings.HasPrefix(item.PlatformThreadID, "local:")
	row = row.MapVariants(func(spec contract.SelectorSpec) contract.SelectorSpec {
		if localKey {
			spec.Target.Texts = append(append([]string(nil), spec.Target.Texts...), item.Name)
		} else {
			attributes := make(map[string]string, len(spec.Target.Attributes)+1)
			for key, value := range spec.Target.Attributes {
				attributes[key] = value
			}
			attributes[thread.ReadAttribute] = item.PlatformThreadID
			spec.Target.Attributes = attributes
		}
		spec.Target.Index = nil
		return spec
	})
	fields := map[string]contract.SelectorSpec{}
	for _, key := range []string{"thread_id", "candidate_id", "name", "avatar_url"} {
		if field, found := cfg.ConversationFields[key]; found {
			fields[key] = field
		}
	}
	rows, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: row, MaxItems: 2, Fields: fields, ExpectedMissing: true})
	if err != nil {
		return snapshot, err
	}
	if len(rows) != 1 || rows[0].TotalMatches > 1 || (!localKey && rows[0].Fields["thread_id"] != item.PlatformThreadID) || strings.TrimSpace(rows[0].Fields["name"]) != item.Name || (item.PlatformCandidateID != "" && rows[0].Fields["candidate_id"] != "" && rows[0].Fields["candidate_id"] != item.PlatformCandidateID) {
		return snapshot, fmt.Errorf("BOSS联系人身份没有唯一匹配，暂不打开")
	}
	if localKey {
		key, keyErr := common.LocalConversationKey("boss", rows[0].Fields["name"], rows[0].Fields["avatar_url"])
		if keyErr != nil || key != item.PlatformThreadID {
			return snapshot, fmt.Errorf("BOSS置顶联系人的姓名或头像已变化，暂不打开")
		}
	}
	action, err := common.RequiredSelector(cfg, "message.contact_click_target")
	if err != nil {
		return snapshot, err
	}
	action = action.WithinSelector(row)
	anchor, err := common.RequiredSelector(cfg, "message.drawer_scroll")
	if err != nil {
		return snapshot, err
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: action, WheelAnchor: &anchor}); err != nil {
		return snapshot, err
	}
	snapshot = model.AutoReplyConversationSnapshot{Conversation: item, CandidateName: item.Name, PlatformThreadID: item.PlatformThreadID, PlatformCandidateID: item.PlatformCandidateID, PlatformAccountID: item.PlatformAccountID, AvatarURL: item.AvatarURL}
	for attempt := 0; attempt < 8; attempt++ {
		name, found, readErr := common.ReadOptional(ctx, browser, cfg, "message.current_name")
		if readErr != nil {
			return snapshot, readErr
		}
		if found && name == item.Name {
			id, readErr := readBossCurrentCandidateID(ctx, browser, cfg)
			if readErr != nil {
				return snapshot, readErr
			}
			if id != "" {
				if item.PlatformCandidateID != "" && id != item.PlatformCandidateID {
					return snapshot, fmt.Errorf("打开后的BOSS候选人编号与列表不符，暂不继续")
				}
				snapshot.PlatformCandidateID = id
				position, _, readErr := common.ReadOptional(ctx, browser, cfg, "message.current_position")
				if readErr != nil {
					return snapshot, readErr
				}
				snapshot.CommunicationPosition = cleanBossAutoReplyPosition(position)
				if item.CommunicationPosition != "" && cleanBossAutoReplyPosition(item.CommunicationPosition) != snapshot.CommunicationPosition {
					return snapshot, fmt.Errorf("打开后的BOSS沟通岗位与列表不符，暂不继续")
				}
				return snapshot, (&Runtime{}).VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
			}
		}
		if err = waitBossReplyPoll(ctx); err != nil {
			return snapshot, err
		}
	}
	return snapshot, errors.New("BOSS聊天头尚未核实，稍后再试")
}

// readBossCurrentCandidateID 交叉核对三个可见原生按钮的ka编号，绝不读取Vue或隐藏状态。
func readBossCurrentCandidateID(ctx context.Context, browser model.Browser, cfg model.Config) (string, error) {
	var result string
	for _, item := range []struct{ key, prefix string }{
		{"message.current_candidate_id", "get_resume_"},
		{"message.current_phone_candidate_id", "get_phone_"},
		{"message.current_wechat_candidate_id", "get_wechat_"},
	} {
		if _, err := common.RequiredSelector(cfg, item.key); err != nil {
			return "", err
		}
		value, found, err := common.ReadOptional(ctx, browser, cfg, item.key)
		if err != nil {
			return "", err
		}
		if !found || !strings.HasPrefix(value, item.prefix) {
			return "", fmt.Errorf("BOSS当前会话的候选人编号还没核实")
		}
		id := strings.TrimPrefix(value, item.prefix)
		if !bossNumericID(id) || (result != "" && result != id) {
			return "", fmt.Errorf("BOSS当前会话的资料按钮编号不一致，暂不操作")
		}
		result = id
	}
	return result, nil
}

// bossCandidateIDFromThread 只解析真实联系人key已验证的数字编号与类型后缀，不把本地路由当平台编号。
func bossCandidateIDFromThread(thread string) string {
	parts := strings.Split(thread, "-")
	if len(parts) != 2 || !bossNumericID(parts[0]) || !bossNumericID(parts[1]) {
		return ""
	}
	return parts[0]
}

// bossNumericID 核对页面原生编号仅由十进制数字组成。
func bossNumericID(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
