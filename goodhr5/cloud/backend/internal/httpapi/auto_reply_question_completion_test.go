// 本文件验证补充成功后工作台自动移出条目，同时保留正文、补答防重、模型题目索引和新问题重开能力。
package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestResolvedQuestionsLeaveWorkspaceWithoutRemovingDelivery 验证移出已完成问题不丢业务数据，旧草稿不能覆盖正文。
func TestResolvedQuestionsLeaveWorkspaceWithoutRemovingDelivery(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx := context.Background()
	if _, err := f.db.Exec("ALTER TABLE positions ADD COLUMN platform_id text NOT NULL DEFAULT 'liepin'"); err != nil {
		t.Fatal(err)
	}
	f.archive(t, "company", "公司办公地址为成都A座。")
	completed := f.question
	for _, view := range []string{"pending", "all"} {
		page, err := f.store.questionInboxPage(ctx, f.tenant, "fact-hr@example.test", f.pos, view, "", 10)
		if err != nil || len(page.Questions) != 0 || page.PendingCount != 0 {
			t.Fatalf("已补充问题仍占工作台：%s %+v %v", view, page, err)
		}
	}
	web, err := f.store.readQuestions(ctx, f.tenant, f.pos, false)
	if err != nil || len(web) != 0 {
		t.Fatal("旧网页入口仍返回已完成问题", web, err)
	}
	index, err := f.store.readQuestions(ctx, f.tenant, f.pos, true)
	if err != nil || len(index) != 1 || index[0].ID != completed.ID || index[0].Answer != "" {
		t.Fatal("移出工作台破坏同义题目索引或暴露旧答案", index, err)
	}
	summary, err := f.store.questionInboxSummary(ctx, f.tenant, "fact-hr@example.test")
	if err != nil || summary.PendingCount != 0 || len(summary.Positions) != 1 || summary.Positions[0].PositionID != f.pos {
		t.Fatal("最后一题完成后丢失当前岗位元数据", summary, err)
	}

	// 显示层移出不能清理待补答或递增版本，尚未送达的事实继续按原发送编号补答一次。
	deliveries, err := f.store.pendingAnswers(ctx, f.tenant, f.user, "device", "liepin", "account")
	if err != nil || len(deliveries) != 1 || deliveries[0].Answer != completed.Answer {
		t.Fatal("补充后自动移出导致候选人漏补答", deliveries, err)
	}
	delivery := deliveries[0]
	claimed, err := f.store.claimAnswer(ctx, f.tenant, f.user, "device", delivery.ID, delivery.ConversationID)
	if err != nil || !claimed {
		t.Fatal("已移出问题不能领取原补答", err)
	}
	f.entity(t, "messages", "outgoing", map[string]string{"conversation_id": "thread", "direction": "self", "text_content": completed.Answer, "platform_message_id": "outgoing-key"})
	if err := f.store.finishAnswer(ctx, f.tenant, f.user, "device", delivery.ID, delivery.ConversationID, "sent", "outgoing-key"); err != nil {
		t.Fatal(err)
	}
	if pending, err := f.store.pendingAnswers(ctx, f.tenant, f.user, "device", "liepin", "account"); err != nil || len(pending) != 0 {
		t.Fatal("已发送补答因条目移出而再次入队", pending, err)
	}

	if q, retry, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, completed.ID, completed.Answer, completed.Version-1); err != nil || retry || q.Version != completed.Version {
		t.Fatal("相同答复重试失去幂等", q, retry, err)
	}
	if _, retry, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, completed.ID, "旧页面试图改成B座", completed.Version); !errors.Is(err, errReplyStateConflict) || retry {
		t.Fatal("旧页面改写了已经移出的完成条目", retry, err)
	}
	company, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || !strings.Contains(company.ExtraInfo, completed.Answer) || strings.Contains(company.ExtraInfo, "B座") {
		t.Fatal("移出条目或旧页面改动破坏公司正文", company.ExtraInfo, err)
	}
	for _, table := range []string{"auto_reply_question_answer_history", "auto_reply_answer_receipts", "auto_reply_cloud_question_sources"} {
		var count int
		if err := f.db.QueryRow("SELECT count(*) FROM "+table+" WHERE question_id=$1", completed.ID).Scan(&count); err != nil || count != 1 {
			t.Fatal("移出工作台清理了审计、防重或来源", table, count, err)
		}
	}
	var messageCount int
	if err := f.db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE kind='messages'").Scan(&messageCount); err != nil || messageCount != 2 {
		t.Fatal("移出工作台丢了原始沟通记录", messageCount, err)
	}

	// 重交旧来源仍为完成状态；只有新的真实未知来信才重新显示同一个问题。
	old, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, completed.ID, "办公地址在哪里", "thread", "k1")
	if err != nil || old.Status != "resolved" || old.Version != completed.Version {
		t.Fatal("旧来源重交让已完成条目重新出现", old, err)
	}
	f.entity(t, "messages", "m2", map[string]string{"conversation_id": "thread", "direction": "candidate", "text_content": "办公地址在哪里？", "platform_message_id": "k2"})
	reopened, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, completed.ID, "办公地址在哪里", "thread", "k2")
	if err != nil || reopened.ID != completed.ID || reopened.Status != "pending" || reopened.Version != completed.Version+1 {
		t.Fatal("新的未知来信未恢复原问题", reopened, err)
	}
	for _, view := range []string{"pending", "all"} {
		page, err := f.store.questionInboxPage(ctx, f.tenant, "fact-hr@example.test", f.pos, view, "", 10)
		if err != nil || len(page.Questions) != 1 || page.Questions[0].ID != completed.ID || page.PendingCount != 1 {
			t.Fatal("新待办恢复时漏行或重复建题", view, page, err)
		}
	}
	if _, retry, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, completed.ID, completed.Answer, completed.Version); !errors.Is(err, errReplyStateConflict) || retry {
		t.Fatal("旧版本重试抢走新来信的待办", retry, err)
	}
}
