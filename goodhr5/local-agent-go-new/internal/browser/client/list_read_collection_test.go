// Package client 文件作用：验证正式名单专用预算、真实请求统计、旧组件兼容及取消后的诊断保留。
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// statsReadRequest 构造不含业务平台名和候选人内容的正式字段请求。
func statsReadRequest() contract.ElementFindAllRequest {
	return contract.ElementFindAllRequest{ReadStats: true, RequestTimeoutMS: 120000, MaxItems: 100,
		Fields: map[string]contract.SelectorSpec{"name": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".name"}}}}}}
}

// TestFormalListReadBudgetOnlyAppliesToRealFields 验证两分钟只用于显式正式批次，并保留普通与特殊操作的旧预算。
func TestFormalListReadBudgetOnlyAppliesToRealFields(t *testing.T) {
	for _, mode := range []string{"formal", "legacy", "probe", "viewport", "no_fields", "default", "negative", "small", "large"} {
		t.Run(mode, func(t *testing.T) {
			request := statsReadRequest()
			want := 45 * time.Second
			switch mode {
			case "formal":
				want = 120 * time.Second
			case "legacy":
				request.ReadStats = false
			case "probe":
				request.ProbeOnly = true
			case "viewport":
				request.ViewportStart = &contract.ViewportStartOptions{}
			case "no_fields":
				request.Fields = nil
			case "default":
				request.RequestTimeoutMS = 0
			case "negative":
				request.RequestTimeoutMS = -1
			case "small":
				request.RequestTimeoutMS = 1
			case "large":
				request.RequestTimeoutMS = int(^uint(0) >> 1)
				want = 180 * time.Second
			}
			if got := requestTimeout(request); got != want {
				t.Fatalf("预算错误：%s，预期%s", got, want)
			}
			wire, err := json.Marshal(request)
			if err != nil || strings.Contains(strings.ToLower(string(wire)), "request_timeout") || strings.Contains(string(wire), "120000") {
				t.Fatalf("Go预算不应改写Worker请求结构：%s，%v", wire, err)
			}
		})
	}
	for _, request := range []any{contract.ElementClickRequest{}, contract.KeyboardPressRequest{}, contract.ScrollRequest{}} {
		if requestTimeout(request) != 45*time.Second {
			t.Fatal("普通点击、按键或滚动预算被延长")
		}
	}
}

// TestListReadCollectionFromHTTP 验证真实HTTP成功、部分超时、零人及坏元数据均不改变原业务结果。
func TestListReadCollectionFromHTTP(t *testing.T) {
	for _, mode := range []string{"success", "empty", "partial_timeout", "old_worker", "bad_optional", "wrong_id", "over_window", "wrong_returned"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				id := r.Header.Get("X-Request-ID")
				budget, _ := strconv.Atoi(r.Header.Get("X-Worker-Timeout-Ms"))
				if !requestIDPattern.MatchString(id) || budget < 117000 || budget > 118000 {
					t.Errorf("真实请求未获得对应编号或118秒预算：%s %d", id, budget)
				}
				if mode == "partial_timeout" {
					_, _ = fmt.Fprintf(w, `{"ok":false,"error":{"code":"WORKER_UNRESPONSIVE","message":"原始读取超时","step":"queue","details":{"request_id":%q,"reason":"deadline_exceeded","request_progress":{"request_id":%q,"worker_phase":"read_field","item_index":41,"read_stats":{"request_id":%q,"planned_count":43,"raw_completed_count":7,"elapsed_ms":43000}}}}}`, id, id, id)
					return
				}
				stats := fmt.Sprintf(`{"request_id":%q,"planned_count":43,"raw_completed_count":43,"returned_count":1,"elapsed_ms":31,"private_value":"不得记录"}`, id)
				data := `[{"index":41,"fields":{"name":"样本"}}]`
				switch mode {
				case "empty":
					stats = fmt.Sprintf(`{"request_id":%q,"planned_count":0,"raw_completed_count":0,"returned_count":0,"elapsed_ms":2}`, id)
					data = `[]`
				case "old_worker":
					stats = `null`
				case "bad_optional":
					stats = `{"planned_count":"bad"}`
				case "wrong_id":
					stats = `{"request_id":"other-request","planned_count":0,"raw_completed_count":0,"elapsed_ms":1}`
				case "over_window":
					stats = fmt.Sprintf(`{"request_id":%q,"planned_count":101,"raw_completed_count":43,"returned_count":1,"elapsed_ms":31}`, id)
				case "wrong_returned":
					stats = fmt.Sprintf(`{"request_id":%q,"planned_count":43,"raw_completed_count":43,"returned_count":2,"elapsed_ms":31}`, id)
				}
				_, _ = fmt.Fprintf(w, `{"ok":true,"data":%s,"read_stats":%s}`, data, stats)
			}))
			defer server.Close()
			ctx, collection := WithListReadCollection(context.Background())
			items, err := New(server.URL).FindAll(ctx, statsReadRequest())
			records := collection.Records()
			if len(records) != 1 || calls != 1 || records[0].RequestID == "" || records[0].Attempt != 1 {
				t.Fatalf("正式请求未被独立收集：%+v，调用%d次", records, calls)
			}
			record := records[0]
			if mode == "partial_timeout" {
				if err == nil || err.Error() != "原始读取超时" || record.Status != "timeout" || record.Stats == nil || *record.Stats.RawCompletedCount != 7 || record.Stats.ReturnedCount != nil {
					t.Fatalf("错误原文或真实部分完成数丢失：%+v，%v", record, err)
				}
				return
			}
			wantCount := 1
			if mode == "empty" {
				wantCount = 0
			}
			if err != nil || len(items) != wantCount || record.Status != "success" {
				t.Fatalf("可选统计改变原data或成功语义：%+v，%v", items, err)
			}
			if mode == "old_worker" || mode == "bad_optional" || mode == "wrong_id" || mode == "over_window" || mode == "wrong_returned" {
				if record.Stats != nil {
					t.Fatalf("未知或不对应的统计不应假填：%+v", record.Stats)
				}
				return
			}
			if record.Stats == nil || record.Stats.PlannedCount == nil || *record.Stats.PlannedCount != map[bool]int{true: 0, false: 43}[mode == "empty"] {
				t.Fatalf("观测到的零或实际数量丢失：%+v", record)
			}
			encoded, _ := json.Marshal(records)
			if strings.Contains(string(encoded), "不得记录") || strings.Contains(string(encoded), "样本") {
				t.Fatal("统计携带了候选人字段值或额外数据")
			}
			*records[0].Stats.PlannedCount = 99
			if *collection.Records()[0].Stats.PlannedCount == 99 {
				t.Fatal("调用方改写了收集器中的历史计数")
			}
		})
	}
}

// TestReadStatsBadProgressKeepsExistingDiagnostic 验证坏统计不会使原有阶段、索引等详细证据丢失。
func TestReadStatsBadProgressKeepsExistingDiagnostic(t *testing.T) {
	var progress contract.RequestDiagnostic
	if err := json.Unmarshal([]byte(`{"request_id":"current","worker_phase":"read_field","item_index":41,"read_stats":{"planned_count":"bad"}}`), &progress); err != nil || progress.WorkerPhase != "read_field" || progress.ItemIndex == nil || *progress.ItemIndex != 41 || progress.ReadStats != nil {
		t.Fatalf("坏可选字段破坏了现有诊断：%+v，%v", progress, err)
	}
}

// TestProbeDoesNotPolluteListReadCollection 验证即使外层收集器存在，探测也不能进入正式人数统计。
func TestProbeDoesNotPolluteListReadCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true,"data":[]}`)) }))
	defer server.Close()
	ctx, collection := WithListReadCollection(context.Background())
	request := statsReadRequest()
	request.ProbeOnly = true
	if _, err := New(server.URL).FindAll(ctx, request); err != nil || len(collection.Records()) != 0 {
		t.Fatalf("探测进入正式统计：%+v，%v", collection.Records(), err)
	}
}
