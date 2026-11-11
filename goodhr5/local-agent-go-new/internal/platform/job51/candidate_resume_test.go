// Package job51 文件作用：验证前程无忧用公开编号恢复位置，换序、缺失或重复时不猜测，追加新人仍可续读。
package job51

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// resumeListHTML 生成与真实编号规则一致的本机夹具，不包含招聘数据。
func resumeListHTML(ids []string) string {
	var out strings.Builder
	for index, id := range ids {
		fmt.Fprintf(&out, `<li id="RESUME_CARD_%s%d">合成人选</li>`, id, index)
	}
	return out.String()
}

// TestJob51ResumeFromStableID 验证已加载100张卡片时只读一次列表HTML恢复到第51张，而非重读100人的五个字段。
func TestJob51ResumeFromStableID(t *testing.T) {
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprint(100000 + i)
	}
	browser := &identityScopeStub{source: resumeListHTML(ids)}
	point := model.CandidateResumePoint{CandidateID: ids[49], PrefixHash: candidatePrefixHash(ids[:50])}
	runtime := NewRuntime()
	cursor := &model.CandidateListCursor{}
	found, err := runtime.RestoreCandidateList(context.Background(), browser, loadPositionConfig(t), point, cursor)
	if err != nil || !found || cursor.StartIndex != 50 || len(browser.scrolls) != 0 {
		t.Fatalf("恢复错误：%v %v %+v", found, err, cursor)
	}
	// 初次读取不等于处理完成；只有主流程索取并保存此断点才推进持久进度。
	candidates := []model.Candidate{{Index: 50, Fingerprint: "51job_100050", Fields: map[string]string{"platform_candidate_id": "100050"}}}
	if err := runtime.rememberResumePoints(context.Background(), browser, loadPositionConfig(t), candidates); err != nil {
		t.Fatal(err)
	}
	next, ok := runtime.CandidateResumePoint(candidates[0])
	if !ok || next.CandidateID != "100050" {
		t.Fatalf("下一断点错误：%+v", next)
	}
}

// TestJob51ResumeFallback 验证名单变化只清空游标，重复编号直接报错，已完成尾行保留给到底判断。
func TestJob51ResumeFallback(t *testing.T) {
	original := []string{"100001", "100002", "100003"}
	point := model.CandidateResumePoint{CandidateID: "100002", PrefixHash: candidatePrefixHash(original[:2])}
	for _, test := range []struct {
		name    string
		ids     []string
		want    bool
		index   int
		wantErr bool
	}{
		{"顺序不变", original, true, 2, false},
		{"尾部追加", append(append([]string{}, original...), "100004"), true, 2, false},
		{"锚点移除", []string{"100001", "100003"}, false, 0, false},
		{"前面换序", []string{"100003", "100002", "100001"}, false, 0, false},
		{"锚点在末尾", original[:2], true, 1, false},
		{"重复编号", []string{"100001", "100002", "100002"}, false, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cursor := &model.CandidateListCursor{StartIndex: 888, IdentityOccurrences: map[string]int{"旧人": 1}}
			found, err := NewRuntime().RestoreCandidateList(context.Background(), &identityScopeStub{source: resumeListHTML(test.ids)}, loadPositionConfig(t), point, cursor)
			if found != test.want || (err != nil) != test.wantErr || cursor.StartIndex != test.index || len(cursor.IdentityOccurrences) != 0 {
				t.Fatalf("错误恢复：found=%v err=%v cursor=%+v", found, err, cursor)
			}
		})
	}
}

// job51BulkResumeBrowser 模拟已加载100张卡片，仅允许名单读取与既有HTML身份核对。
type job51BulkResumeBrowser struct {
	identityScopeStub
	ids     []string
	maxRead int
}

// FindAll 按实际全局行号和配置批量返回卡片，加载标记为空，不允许把100人拼成一次读取。
func (b *job51BulkResumeBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if len(req.Fields) == 0 {
		return nil, nil
	}
	b.maxRead = max(b.maxRead, req.MaxItems)
	rows := []contract.FindAllItem{}
	for i := req.StartIndex; i < min(len(b.ids), req.StartIndex+req.MaxItems); i++ {
		rows = append(rows, contract.FindAllItem{Index: i, TotalMatches: len(b.ids), Text: fmt.Sprintf("合成人选%d 30岁 本科", i), Fields: map[string]string{"name": fmt.Sprintf("合成人选%d", i), "basic_info": "30岁 本科", "platform_candidate_id": fmt.Sprintf("RESUME_CARD_%s%d", b.ids[i], i)}})
	}
	return rows, nil
}

// TestJob51LoadedHundredReadsAllInSmallBatches 验证100张已加载卡片完整读完且每批20人，重建运行时仍从下一个编号继续，不重复滚轮或漏掉后半批。
func TestJob51LoadedHundredReadsAllInSmallBatches(t *testing.T) {
	cfg := loadPositionConfig(t)
	browser := &job51BulkResumeBrowser{ids: make([]string, 100)}
	for i := range browser.ids {
		browser.ids[i] = fmt.Sprint(200000 + i)
	}
	browser.source = resumeListHTML(browser.ids)
	cursor := &model.CandidateListCursor{}
	runtime := NewRuntime()
	_, _ = runtime.RestoreCandidateList(context.Background(), browser, cfg, model.CandidateResumePoint{}, cursor)
	read := 0
	for batch := 0; batch < 5; batch++ {
		rows, err := runtime.FindCandidates(context.Background(), browser, cfg, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
		if err != nil || len(rows) != 20 || rows[0].Index != read {
			t.Fatalf("窗口错误：batch%d rows%d cursor%+v err%v", batch, len(rows), cursor, err)
		}
		for _, row := range rows {
			if row.Fields["platform_candidate_id"] != browser.ids[read] {
				t.Fatal("重启后重复或漏人")
			}
			read++
		}
		if batch == 0 {
			point, ok := runtime.CandidateResumePoint(rows[len(rows)-1])
			if !ok {
				t.Fatal("没有断点")
			}
			runtime = NewRuntime()
			found, err := runtime.RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
			if err != nil || !found || cursor.StartIndex != 20 {
				t.Fatalf("重启没有从下一人继续：%v %v %+v", found, err, cursor)
			}
		} else if batch < 4 {
			advanced, err := runtime.AdvanceCandidateList(context.Background(), browser, cfg, rows, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
			if err != nil || !advanced {
				t.Fatalf("后半批推进失败：%v %v", advanced, err)
			}
		}
	}
	if read != 100 || browser.maxRead > 20 || len(browser.scrolls) > 0 {
		t.Fatalf("读取量或滚轮错误：已读%d 单次%d 滚轮%d", read, browser.maxRead, len(browser.scrolls))
	}
}
