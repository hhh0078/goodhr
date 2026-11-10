// Package liepin 本文件验证已读会话重启恢复不依赖未读角标，且只从全部联系人按稳定编号重新定位。
package liepin

import (
	"context"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestOpenAutoReplyConversationRestoresWithoutUnreadBadge 验证抽屉已关且未读为零时仍能恢复已知会话并读取历史。
func TestOpenAutoReplyConversationRestoresWithoutUnreadBadge(t *testing.T) {
	cfg := historyAuditConfig()
	for key, description := range map[string]string{
		"message.drawer": "测试联系人抽屉", "message.entry": "测试联系人入口",
		"message.entry_unread_count": "测试未读数字", "message.all_tab": "测试全部标签",
		"message.all_tab_selected": "测试全部标签已选中", "message.contact_item": "测试联系人",
		"message.contact_click_target": "message.contact_click_target", "message.drawer_scroll": "测试联系人滚动区域",
		"message.current_name": "测试当前候选人", "message.current_position": "测试当前岗位",
		"message.candidate_id_source": "测试候选人编号",
	} {
		cfg.Selectors[key] = testLiepinSelector(description)
	}
	browser := &liepinReplyBrowserStub{items: []contract.FindAllItem{{Index: 0, Fields: map[string]string{"name": "邓云川", "thread_meta": `{"to_imid":"thread"}`}}}, historyBatches: [][]contract.FindAllItem{{historyAuditMessage("latest")}}}
	snapshot, err := (&Runtime{}).OpenAutoReplyConversation(context.Background(), browser, cfg, model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "邓云川"}, nil, 5000)
	if err != nil || snapshot.PlatformThreadID != "thread" || len(snapshot.Messages) != 1 || !browser.drawerOpen || !browser.allSelected || browser.unreadSelected {
		t.Fatalf("没有未读角标时无法恢复已知会话：snapshot=%+v err=%v clicks=%v", snapshot, err, browser.clicks)
	}
}
