// Package boss 本文件验证生产来源选择器先固定CSS原行再核对文字，并提供显式启用的真实Worker只查找验证。
package boss

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBossPinnedSelectorsFixPositionBeforeFiltering 验证同意及PDF共享的生产行选择器不再携带过滤后全局序号。
func TestBossPinnedSelectorsFixPositionBeforeFiltering(t *testing.T) {
	b, cfg, _ := bossResumeOfferEnvironment(t)
	item := b.messages[2]
	item.Index, item.TotalMatches = 2, 4
	button, err := pinBossResumeOfferButton(cfg, item, "10101")
	if err != nil {
		t.Fatal(err)
	}
	pdfRow, err := pinBossMessageRow(cfg, item, "唯一附件.pdf", "10101")
	if err != nil {
		t.Fatal(err)
	}
	pdf := bossReplySelector("pdf-entry").WithinSelector(pdfRow)
	for _, spec := range []contract.SelectorSpec{button, pdf} {
		row := spec.Parents[len(spec.Parents)-1]
		if row.Index != nil || row.Selectors[0].Value != ":nth-match(.message-item, 3)" || len(row.Texts) == 0 {
			t.Fatal("原消息行仍在文本过滤后应用全局Index，或者丢失原文核对")
		}
	}
	if path := os.Getenv("GOODHR_BOSS_PIN_SELECTOR_OUTPUT"); path != "" {
		raw, err := json.Marshal(map[string]contract.SelectorSpec{"offer": button, "resume": pdf})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBossOfferClickRequiresExplicitNotStarted 验证只有完整的查找阶段未开始点击证明才能暂缓，含糊NotFound和迟到失败不得释放。
func TestBossOfferClickRequiresExplicitNotStarted(t *testing.T) {
	for _, test := range []struct {
		name, action, code, step, details string
		want                              bool
	}{
		{"proven", "element.click", "ELEMENT_NOT_FOUND", "find", `{"click_started":false}`, true},
		{"already_started", "element.click", "ELEMENT_NOT_FOUND", "find", `{"click_started":true}`, false},
		{"missing_proof", "element.click", "ELEMENT_NOT_FOUND", "find", `{}`, false},
		{"verify_after_click", "element.click", "ELEMENT_NOT_FOUND", "verify_click", `{"click_started":false}`, false},
		{"move", "element.click", "ELEMENT_NOT_FOUND", "move", `{"click_started":false}`, false},
		{"read", "element.read", "ELEMENT_NOT_FOUND", "find", `{"click_started":false}`, false},
		{"timeout", "element.click", "ACTION_TIMEOUT", "find", `{"click_started":false}`, false},
		{"invalid_proof", "element.click", "ELEMENT_NOT_FOUND", "find", `{"click_started":"false"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := &contract.WorkerError{Body: contract.WorkerErrorBody{Action: test.action, Code: test.code, Step: test.step, Details: json.RawMessage(test.details)}}
			if bossOfferClickNotStarted(err) != test.want {
				t.Fatal("未开始点击的证明边界不正确")
			}
			b, cfg, snapshot := bossResumeOfferEnvironment(t)
			b.clickErr = err
			result, requestErr := requestBossAutoReplyCandidateInfo(context.Background(), b, cfg, snapshot, model.CandidateInfoRequest{RequestResume: true})
			if b.accepts != 1 || (requestErr == nil) != test.want || test.want && result.Status != model.AutoReplyInfoDeferred {
				t.Fatal("平台没有按真实输入证明暂缓，或重复尝试同意")
			}
		})
	}
}

// TestBossSourcePinLiveFindOnly 使用显式本机Worker只读实际原生邀约并查找同意按钮，绝不点击、滚动、发消息或改变页面。
func TestBossSourcePinLiveFindOnly(t *testing.T) {
	workerURL := os.Getenv("GOODHR_BOSS_SOURCE_PIN_FIND_WORKER")
	if workerURL == "" {
		t.Skip("真实来源查找仅在显式指定隔离Worker及候选编号时启用")
	}
	u, err := url.Parse(workerURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Hostname() != "127.0.0.1" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("只允许显式指定本机隔离Worker根地址")
	}
	candidateID := os.Getenv("GOODHR_BOSS_SOURCE_PIN_CANDIDATE")
	if !bossNumericID(candidateID) {
		t.Fatal("必须显式提供已核实的候选原生编号")
	}
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser := client.New(workerURL)
	items, err := common.ReadConfiguredConversationMessages(ctx, browser, cfg)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := bossMessages(items, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, message := range messages {
		if bossResumeOfferMessage(message) {
			if index != -1 {
				t.Fatal("当前有多个邀约，干跑不自行选择")
			}
			index = i
		}
	}
	if index < 0 {
		t.Fatal("没有真实原生邀约，未执行任何点击")
	}
	spec, err := pinBossResumeOfferButton(cfg, items[index], candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("GOODHR_BOSS_SOURCE_PIN_FIND_PROOF"); path != "" {
		proof, _ := json.Marshal(map[string]any{"clicked": false, "message_index": items[index].Index, "message_count": items[index].TotalMatches, "selector": spec})
		if err = os.WriteFile(path, proof, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldRaw, _ := json.Marshal(spec)
	var old contract.SelectorSpec
	_ = json.Unmarshal(oldRaw, &old)
	oldRow := &old.Parents[len(old.Parents)-1]
	oldRow.Selectors[0].Value = cfg.Selectors["message.item"].Target.Selectors[0].Value
	oldRow.Index = &items[index].Index
	_, oldErr := browser.Find(ctx, contract.ElementFindRequest{Selector: old})
	var oldWorker *contract.WorkerError
	if oldErr == nil || !errors.As(oldErr, &oldWorker) || oldWorker.Body.Code != "ELEMENT_NOT_FOUND" {
		t.Fatal("旧序号与文本组合没有按真实Worker复现问题，未执行任何点击")
	}
	found, err := browser.Find(ctx, contract.ElementFindRequest{Selector: spec})
	if err != nil || strings.TrimSpace(found.ElementRef) == "" {
		t.Fatalf("修复后的真实来源按钮尚不能只读定位：%v", err)
	}
	if path := os.Getenv("GOODHR_BOSS_SOURCE_PIN_FIND_PROOF"); path != "" {
		proof, _ := json.Marshal(map[string]any{"old_code": oldWorker.Body.Code, "old_step": oldWorker.Body.Step, "new_found": true, "clicked": false, "message_index": items[index].Index, "message_count": items[index].TotalMatches, "selector": spec})
		if err = os.WriteFile(path, proof, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("旧定位失败，修复后的唯一同意按钮只读定位成功；没有点击、滚动或发消息")
}
