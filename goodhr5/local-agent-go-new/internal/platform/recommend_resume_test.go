// Package boss_test 文件作用：通过正式智联适配验证推荐名单换序、分批加载与同名同龄序号恢复，不访问账号。
package platform_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// recommendResumeBrowser 仅允许合成卡片读取和滚轮，其他浏览器能力不可调用。
type recommendResumeBrowser struct {
	model.Browser
	rows                            []map[string]string
	loaded, scrolls, maxRead, reads int
	scrollErr                       error
}

// FindAll 按真实全局索引和尾部窗口返回请求字段，动态活跃文字不能成为身份依据。
func (b *recommendResumeBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.reads++
	b.maxRead = max(b.maxRead, req.MaxItems)
	start := req.StartIndex
	if req.FromEnd {
		start = max(0, b.loaded-req.MaxItems)
	}
	var result []contract.FindAllItem
	for index := start; index < min(b.loaded, start+req.MaxItems); index++ {
		fields := make(map[string]string)
		for key := range req.Fields {
			fields[key] = b.rows[index][key]
		}
		result = append(result, contract.FindAllItem{Index: index, TotalMatches: b.loaded, Fields: fields, Text: fmt.Sprintf("%s 30岁 活跃状态%d", b.rows[index]["name"], b.reads)})
	}
	return result, nil
}

// Scroll 模拟原有真实滚轮链路每次追加20人，并保留位置证明或返回真实错误。
func (b *recommendResumeBrowser) Scroll(ctx context.Context, req contract.ScrollRequest) (contract.ScrollResult, error) {
	if err := ctx.Err(); err != nil {
		return contract.ScrollResult{}, err
	}
	b.scrolls++
	if b.scrollErr != nil {
		return contract.ScrollResult{}, b.scrollErr
	}
	if req.WheelAnchor == nil || req.ProgressTarget == nil {
		return contract.ScrollResult{}, errors.New("续跑加载没有绑定原列表和尾部")
	}
	moved := b.loaded < len(b.rows)
	b.loaded = min(len(b.rows), b.loaded+20)
	yes := true
	return contract.ScrollResult{ProgressTargetVisible: &yes, ProgressTargetMoved: &moved}, nil
}

// TestRecommendResumeDistinguishesEmptyLoadingAndMissing 验证空白未就绪不清断点，真正抵达稳定底部且没有锚点才从头；活跃文字变化不冒充新名单。
func TestRecommendResumeDistinguishesEmptyLoadingAndMissing(t *testing.T) {
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	point := model.CandidateResumePoint{CandidateID: "合成不在名单的身份", PrefixHash: "合成旧前缀"}
	for _, id := range []string{"zhaopin"} {
		cfg, err := platform.LoadConfig(id)
		if err != nil {
			t.Fatal(err)
		}
		found, err := recommendResumeRuntime(t, id).RestoreCandidateList(context.Background(), &recommendResumeBrowser{}, cfg, point, &model.CandidateListCursor{})
		if err == nil || found {
			t.Fatal("空白加载被误判为断点缺失")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	browser := &recommendResumeBrowser{rows: recommendResumeRows(3), loaded: 3}
	cursor := &model.CandidateListCursor{StartIndex: 99}
	found, err := recommendResumeRuntime(t, "zhaopin").RestoreCandidateList(ctx, browser, cfg, point, cursor)
	if err != nil || found || cursor.StartIndex != 0 || browser.scrolls != 3 {
		t.Fatalf("没有等稳定底部或没有安全从头：%v %v %+v scrolls=%d", found, err, cursor, browser.scrolls)
	}
}

// recommendResumeRows 使用既有姓名与年龄学历字段生成可核对的联合身份，完整卡片只供原适配完整性校验。
func recommendResumeRows(count int) []map[string]string {
	rows := make([]map[string]string, count)
	for index := range rows {
		rows[index] = map[string]string{"name": fmt.Sprintf("合成人选%d", index), "education": fmt.Sprintf("30岁 %d年 本科", index), "card_text": "本机合成卡片 30岁", "basic_info": "30岁 本科"}
	}
	return rows
}

// TestRecommendResumeLoadsBeyondFirstBatch 验证第46人不在首批时沿原滚轮寻找，每批不超过20人，断线不冒充找不到。
func TestRecommendResumeLoadsBeyondFirstBatch(t *testing.T) {
	for _, id := range []string{"zhaopin"} {
		t.Run(id, func(t *testing.T) {
			cfg, err := platform.LoadConfig(id)
			if err != nil {
				t.Fatal(err)
			}
			cfg.MaxItems = 20 // 夹具分批建立60人的历史断点；恢复自身的20人上限另行核对。
			rows := recommendResumeRows(60)
			browser := &recommendResumeBrowser{rows: rows, loaded: 60}
			runtime := recommendResumeRuntime(t, id)
			cursor := &model.CandidateListCursor{}
			_, _ = runtime.RestoreCandidateList(context.Background(), browser, cfg, model.CandidateResumePoint{}, cursor)
			var point model.CandidateResumePoint
			for start := 0; start < 60; start += 20 {
				cursor.StartIndex = start
				batch, err := runtime.FindCandidates(context.Background(), browser, cfg, model.CandidateListOptions{Cursor: cursor})
				if err != nil || len(batch) != 20 {
					t.Fatalf("保存前批次错误：%v %d", err, len(batch))
				}
				if start == 40 {
					point, _ = runtime.CandidateResumePoint(batch[5])
				}
			}
			for _, failure := range []error{nil, errors.New("合成滚轮断开")} {
				browser = &recommendResumeBrowser{rows: rows, loaded: 20, scrollErr: failure}
				cursor = &model.CandidateListCursor{}
				found, err := recommendResumeRuntime(t, id).RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
				if failure != nil {
					if found || !errors.Is(err, failure) {
						t.Fatalf("断开被误判为缺失：%v %v", found, err)
					}
				} else if err != nil || !found || cursor.StartIndex != 45 || browser.scrolls != 2 || browser.maxRead > 20 {
					t.Fatalf("未找到首批之后的位置：%v %v %+v scrolls=%d max=%d", found, err, cursor, browser.scrolls, browser.maxRead)
				}
			}
		})
	}
}

// TestRecommendResumeRestoresOccurrenceAfterReorder 验证前面换序不重置唯一锚点，并还原同名同龄者的真实序号；完全重复身份仍从头。
func TestRecommendResumeRestoresOccurrenceAfterReorder(t *testing.T) {
	for _, id := range []string{"zhaopin"} {
		t.Run(id, func(t *testing.T) {
			cfg, err := platform.LoadConfig(id)
			if err != nil {
				t.Fatal(err)
			}
			rows := recommendResumeRows(3)
			rows[0]["name"], rows[1]["name"] = "同名人选", "同名人选"
			browser := &recommendResumeBrowser{rows: rows, loaded: 3}
			runtime := recommendResumeRuntime(t, id)
			cursor := &model.CandidateListCursor{}
			_, _ = runtime.RestoreCandidateList(context.Background(), browser, cfg, model.CandidateResumePoint{}, cursor)
			batch, err := runtime.FindCandidates(context.Background(), browser, cfg, model.CandidateListOptions{Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			point, _ := runtime.CandidateResumePoint(batch[1])
			browser.rows = []map[string]string{rows[2], rows[0], rows[1]}
			runtime = recommendResumeRuntime(t, id)
			found, err := runtime.RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
			if err != nil || !found || cursor.StartIndex != 2 {
				t.Fatalf("推荐换序导致从头：%v %v %+v", found, err, cursor)
			}
			window := cfg
			window.MaxItems = 1
			batch, err = runtime.FindCandidates(context.Background(), browser, window, model.CandidateListOptions{Cursor: cursor})
			if err != nil || len(batch) != 1 || batch[0].IdentityOccurrence != 1 {
				t.Fatalf("同名同龄序号没有恢复：%v %+v", err, batch)
			}
			action, err := common.CandidateActionSelector(cfg, "candidate.open_target", batch[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, variant := range action.Variants() {
				for _, card := range variant.Parents {
					if len(card.Texts) > 0 && (card.Index == nil || *card.Index != 1) {
						t.Fatal("恢复后仍指向第一个同名人选")
					}
				}
			}
			browser.rows = []map[string]string{rows[1], rows[1], rows[2]}
			found, err = recommendResumeRuntime(t, id).RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
			if err != nil || found || cursor.StartIndex != 0 {
				t.Fatal("重复联合身份不能猜测断点")
			}
		})
	}
}

// recommendResumeTestRuntime 保留正式平台流程及断点能力，防止夹具绕过实际适配。
type recommendResumeTestRuntime interface {
	model.Runtime
	model.CandidateListResumer
}

// recommendResumeRuntime 从正式注册表创建平台，验证推荐页没有丢失断点能力。
func recommendResumeRuntime(t *testing.T, id string) recommendResumeTestRuntime {
	t.Helper()
	runtime, err := platform.RuntimeFor(id)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := runtime.(recommendResumeTestRuntime)
	if !ok {
		t.Fatal("正式平台缺少断点能力")
	}
	return result
}
