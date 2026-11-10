// Package platform 本文件验证BOSS真实页面配置的核身、动作回执和附件来源边界，防止旧选择器漏读或误发。
package platform

import (
	"os"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBossAutoReplyConfigUsesVisibleIdentityAndAllContacts 验证普通行和置顶行都可发现，核身不依赖不存在的原生消息ID。
func TestBossAutoReplyConfigUsesVisibleIdentityAndAllContacts(t *testing.T) {
	cfg := loadBossActionConfig(t)
	if !cfg.Behavior.AutoReplyAllContactInbox || !cfg.Behavior.AutoReplyNativeInfoRequests || !cfg.Behavior.SupportsPostDeletionHistory || !cfg.Behavior.AutoReplyResumeLoadsHistory || cfg.Behavior.AutoReplyMessageIdentityVersion != 1 {
		t.Fatal("全部联系人、原生索要、删除后边界和稳定时间组消息键必须同时启用")
	}
	for key, marker := range map[string]string{
		"message.current_candidate_id":        "iboss-editor-resume[ka^=\"get_resume_\"]",
		"message.current_phone_candidate_id":  "iboss-phone[ka^=\"get_phone_\"]",
		"message.current_wechat_candidate_id": "iboss-wechat[ka^=\"get_wechat_\"]",
	} {
		spec := cfg.Selectors[key]
		if spec.ReadAttribute != "ka" || !strings.Contains(bossActionCSS(spec), marker) || !strings.Contains(bossActionParents(spec), ".chat-global-conversation") {
			t.Fatalf("%s须限定当前聊天和对应按钮类别后读取公开ka", key)
		}
	}
	name := bossActionCSS(cfg.Selectors["message.current_name"])
	if !strings.Contains(name, ".chatview-name") || !strings.Contains(name, ".bosschat-geek-detail .detail-name") {
		t.Fatal("折叠和展开的候选人头部必须共用同一姓名核验")
	}
	thread := cfg.Selectors["message.current_thread_id"]
	if thread.ReadAttribute != "key" || !strings.Contains(bossActionCSS(thread), "[role=\"listitem\"]:has(.friend-list-item.active)") {
		t.Fatal("当前会话须从活动联系人的外层key核验")
	}
	contact := bossActionCSS(cfg.Selectors["message.contact_item"])
	if !strings.Contains(contact, "[role=\"listitem\"][key]") || !strings.Contains(contact, "[role=\"thead\"] .friend-list-item") {
		t.Fatal("普通联系人须读外层key，同时保留置顶无key行")
	}
	field := cfg.ConversationFields["thread_id"]
	if field.ReadAttribute != "key" || bossActionCSS(field) != ":scope" {
		t.Fatal("列表会话编号须读取本行公开key")
	}
	if _, exists := cfg.ConversationFields["message_id"]; exists {
		t.Fatal("真实页面没有原生消息ID，不能虚构该字段")
	}
	// message_meta只读取实测原生卡图标的公开类名，不能当成消息编号或读取平台隐藏状态。
	meta := cfg.ConversationFields["message_meta"]
	if meta.ReadAttribute != "class" || len(meta.Target.Selectors) != 2 {
		t.Fatal("原生资料元数据只能读取两类已核实卡片的图标class")
	}
	contactIcon, offerIcon := meta.Target.Selectors[0].Value, meta.Target.Selectors[1].Value
	if !strings.Contains(contactIcon, ".item-system .contact-copy") || !strings.Contains(contactIcon, ".message-dialog-icon-contact") || !strings.Contains(offerIcon, ".item-friend .message-card-wrap") || !strings.Contains(offerIcon, "对方想发送附件简历给您，您是否同意") || !strings.Contains(offerIcon, "61031") || !strings.Contains(offerIcon, ".message-dialog-icon-resume") {
		t.Fatal("原生图标必须限定系统联系方式卡或当前候选人的真实附件邀约")
	}
	if !strings.Contains(bossActionCSS(cfg.ConversationFields["message_text"]), ".item-system .text > span") {
		t.Fatal("原生请求系统回执必须有可读取的正文")
	}
}

// TestBossAutoReplyConfigKeepsShortcutsAndPreciseDownload 验证快捷键及备用按钮完整，附件来源仅从当前预览读取。
func TestBossAutoReplyConfigKeepsShortcutsAndPreciseDownload(t *testing.T) {
	cfg := loadBossActionConfig(t)
	if !cfg.Behavior.AutoReplySendEnterFirst || !cfg.Behavior.MessageSendEnterFallback || !cfg.Behavior.DetailCloseEscapeFirst {
		t.Fatal("发送与关闭必须保留快捷键优先和明确未开始时的备用能力")
	}
	if !cfg.Behavior.AutoReplySingleLineMessages {
		t.Fatal("BOSS须先把换行变成空格定稿，避免平台发送时直接删除换行连词")
	}
	send := bossActionCSS(cfg.Selectors["message.send"])
	if !strings.Contains(send, ".submit-content .submit.active") || !strings.Contains(send, ".btn-send:not(.btn-disabled)") {
		t.Fatal("回车未开始时仍须兼容新版和旧版可用发送按钮")
	}
	if bossActionCSS(cfg.Selectors["message.drawer_collapse"]) != ".chat-global-main > .chat-global-top .back" {
		t.Fatal("一键收起须使用列表左上角真实按钮，不能误用聊天关闭图标")
	}
	for _, kind := range []string{"resume", "phone", "wechat"} {
		button := cfg.Selectors["candidate.request_"+kind]
		confirm := cfg.Selectors["candidate.request_"+kind+"_confirm"]
		if !strings.Contains(bossActionCSS(button), ":not(.disabled)") || button.State != "enabled" || !strings.Contains(bossActionCSS(confirm), ".boss-btn-primary") {
			t.Fatalf("%s原生索要须排除禁用状态并保留确认按钮", kind)
		}
	}
	if cfg.Behavior.AttachmentMode != "preview_download" || cfg.Behavior.AttachmentAllowedScheme != "https" || cfg.Behavior.AttachmentAllowedHost != "docdownload.zhipin.com" || cfg.Behavior.AttachmentAllowedPath != "/wflow/zpgeek/download/download4boss/" || cfg.Behavior.AttachmentPreviewPath != "/wflow/zpgeek/download/preview4boss/" {
		t.Fatal("附件只允许已逐值验证的预览与下载路径")
	}
	source := cfg.Selectors["message.attachment_file_source"]
	if source.ReadAttribute != "src" || bossActionCSS(source) != "iframe.attachment-iframe" || !strings.Contains(bossActionParents(source), ".dialog-wrap.active:has(iframe.attachment-iframe)") {
		t.Fatal("附件来源必须属于当前激活预览的真实iframe")
	}
}

// TestBossOnlineProfileConfigUsesCanvasAndCurrentModal 验证在线截图限定当前弹层，姓名由当前聊天及服务器AI分别核验。
func TestBossOnlineProfileConfigUsesCanvasAndCurrentModal(t *testing.T) {
	cfg := loadBossActionConfig(t)
	entry := cfg.Selectors["message.online_profile_entry"]
	if len(entry.Target.Selectors) != 2 || entry.Target.Selectors[0].Value != ".bosschat-chat-global-card .user-info .resume-icon" || entry.Target.Selectors[1].Value != ".bosschat-geek-detail .geek-resume" || !strings.Contains(bossActionParents(entry), ".chat-global-conversation") {
		t.Fatal("在线简历先用已实测的紧凑头部直接入口，保留展开头部备用")
	}
	if bossActionCSS(cfg.Selectors["message.online_profile_panel"]) != ".new-resume-online-main-ui:visible" || !strings.Contains(bossActionCSS(cfg.Selectors["message.online_profile_close"]), ".new-resume-online-main-ui:visible .close-btn") {
		t.Fatal("在线简历必须定位真实可见弹层及自身备用关闭按钮")
	}
	frame := cfg.Selectors["message.online_profile_frame_url"]
	if frame.ReadAttribute != "src" || !strings.Contains(bossActionCSS(frame), ".new-resume-online-main-ui:visible iframe[src*='/web/frame/c-resume/']:visible") {
		t.Fatal("在线截图前后须读取本次可见框架公开来源")
	}
	shot := cfg.Selectors["message.online_profile_screenshot"]
	if len(shot.Frames) != 1 || len(shot.FrameFallbacks) != 0 || !strings.Contains(shot.Frames[0].Selectors[0].Value, ".new-resume-online-main-ui:visible") || !strings.Contains(bossActionCSS(shot), "#resume") {
		t.Fatal("截图不能降级到旁边推荐候选人的详情框架")
	}
	if _, exists := cfg.Selectors["message.online_profile_body"]; exists {
		t.Fatal("canvas在线简历不能虚构DOM正文")
	}
	if !strings.Contains(bossActionCSS(cfg.Selectors["message.online_profile_name"]), ".detail-name") {
		t.Fatal("截图前须保留当前聊天候选人姓名核验")
	}
}

// loadBossActionConfig 加载发布时实际嵌入的配置，并检查中文说明和既有任务契约。
func loadBossActionConfig(t *testing.T) model.Config {
	t.Helper()
	content, err := os.ReadFile("boss/config.json")
	if err != nil {
		t.Fatal(err)
	}
	assertConfigComments(t, content)
	cfg, err := LoadConfig("boss")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskConfig(cfg, "auto_reply"); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// bossActionCSS 收集目标配置，便于检查同一能力的已确认兼容分支。
func bossActionCSS(spec contract.SelectorSpec) string {
	var values []string
	for _, item := range spec.Target.Selectors {
		values = append(values, item.Value)
	}
	return strings.Join(values, "\n")
}

// bossActionParents 收集父级范围，避免测试只验证叶节点而遗漏候选人隔离。
func bossActionParents(spec contract.SelectorSpec) string {
	var values []string
	for _, group := range spec.Parents {
		for _, item := range group.Selectors {
			values = append(values, item.Value)
		}
	}
	return strings.Join(values, "\n")
}
