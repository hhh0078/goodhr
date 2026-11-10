// 本文件验证用户主动确认结束教程的原句、账号隔离和数据库持久化，不伪造招聘运行的完成事实。
package httpapi

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// TestTutorialAcknowledgmentRequiresExactPhrase 验证错误文字不能结束教程，正确声明只更新本人教程完成标记。
func TestTutorialAcknowledgmentRequiresExactPhrase(t *testing.T) {
	store := NewMemoryUserFlowStore()
	for _, text := range []string{"", "我清楚了", "我已经清楚GOODHR的基础操作", "我已经清楚goodhr的基础操作。"} {
		_, err := store.Record("ack@example.test", UserFlowUpdate{Step: "tutorial_acknowledged", Status: "completed", Metadata: map[string]any{"confirmation": text}})
		if err == nil {
			t.Fatalf("错误原句被接受：%q", text)
		}
	}
	state, err := store.Record("ack@example.test", UserFlowUpdate{Step: "tutorial_acknowledged", Status: "completed", Metadata: map[string]any{"confirmation": "我已经清楚goodhr的基础操作"}})
	if err != nil || !state.TutorialCompleted || state.State == "completed" || state.CompletedAt != nil {
		t.Fatalf("教程声明改变了真实招聘完成状态：%+v %v", state, err)
	}
	for key, step := range state.Steps {
		if step.Status == "completed" {
			t.Fatalf("伪造了招聘节点：%s", key)
		}
	}
	other, _ := store.Get("other@example.test")
	if other.TutorialCompleted {
		t.Fatal("其他账号继承了主动结束状态")
	}
}

// TestTutorialAcknowledgmentPostgres 验证声明经既有流程接口存储后可重新读取，且真实新用户漏斗保持原样。
func TestTutorialAcknowledgmentPostgres(t *testing.T) {
	db := statisticsPostgresFixture(t)
	store := NewPostgresUserFlowStore(db)
	if _, err := store.Get("tutorial-ack@example.test"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stats := &postgresSystemStatistics{db: db}
	before, err := stats.Summary(context.Background(), aiUsageDay(now), aiUsageDay(now).AddDate(0, 0, 1), now)
	if err != nil {
		t.Fatal(err)
	}
	update := UserFlowUpdate{Step: "tutorial_acknowledged", Status: "completed", Source: "interactive_tutorial", Metadata: map[string]any{"confirmation": "我已经清楚goodhr的基础操作"}}
	if _, err = store.Record("tutorial-ack@example.test", update); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Get("tutorial-ack@example.test")
	if err != nil || !saved.TutorialCompleted || saved.State == "completed" || saved.Steps[userFlowFirstScreeningResult].Status == "completed" {
		t.Fatalf("数据库保存结果不正确：%+v %v", saved, err)
	}
	after, err := stats.Summary(context.Background(), aiUsageDay(now), aiUsageDay(now).AddDate(0, 0, 1), now)
	if err != nil || !reflect.DeepEqual(before.Funnel, after.Funnel) {
		t.Fatalf("主动结束教程伪造了新用户漏斗：%v", err)
	}
}
