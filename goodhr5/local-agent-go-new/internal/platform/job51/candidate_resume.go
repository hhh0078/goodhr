// Package job51 文件作用：推荐页通过标准列表HTML公开编号恢复断点，搜索页保留名单前缀保护。
package job51

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// RestoreCandidateList 推荐页先按唯一公开编号恢复；搜索页保留完整前缀核对，未加载的断点使用真实滚轮继续查找。
// 只修改任务游标与临时滚动位置，不点击、切岗或访问页面脚本状态；三个桌面目标共用实现。
func (r *Runtime) RestoreCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, point model.CandidateResumePoint, cursor *model.CandidateListCursor) (bool, error) {
	r.resumeTracking, r.resumePoints = true, nil
	if cursor == nil {
		return false, fmt.Errorf("候选人读取位置还没准备好")
	}
	*cursor = model.CandidateListCursor{}
	if !numericCandidateID(point.CandidateID) || len(point.PrefixHash) != 64 {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	loadNext := cfg.Behavior.CandidateResumeByIdentity
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		ids, err := readResumeCandidateIDs(ctx, browser, cfg)
		if err != nil && !common.IsElementMissing(err) {
			return false, err
		}
		for index, id := range ids {
			if id != point.CandidateID {
				continue
			}
			if (!cfg.Behavior.CandidateResumeByIdentity || r.manualSearch) && candidatePrefixHash(ids[:index+1]) != point.PrefixHash {
				return false, nil
			}
			cursor.StartIndex = index + 1
			if cursor.StartIndex == len(ids) {
				cursor.StartIndex = index
			}
			return true, nil
		}
		if !loadNext {
			return false, nil
		}
		loadNext, err = common.LoadMoreResumeCandidates(ctx, identityBrowser{browser}, cfg)
		if err != nil {
			return false, err
		}
	}
}

// CandidateResumePoint 返回本批读取时核对过的编号与顺序摘要；是否完成由主流程决定，预读取本身不推进进度。
func (r *Runtime) CandidateResumePoint(candidate model.Candidate) (model.CandidateResumePoint, bool) {
	point, ok := r.resumePoints[candidate.Fingerprint]
	return point, ok
}

// rememberResumePoints 在完整小批读取后核对编号和真实行号，只记录本批候选人的只读断点。
func (r *Runtime) rememberResumePoints(ctx context.Context, browser model.Browser, cfg model.Config, candidates []model.Candidate) error {
	r.resumePoints = nil
	if !r.resumeTracking || len(candidates) == 0 {
		return nil
	}
	ids, err := readResumeCandidateIDs(ctx, browser, cfg)
	if err != nil {
		return err
	}
	points := make(map[string]model.CandidateResumePoint, len(candidates))
	for _, candidate := range candidates {
		index := candidate.Index
		if index < 0 || index >= len(ids) || ids[index] != candidate.Fields["platform_candidate_id"] {
			return fmt.Errorf("前程无忧名单刚刚变了，这批先不操作，请重新开始")
		}
		points[candidate.Fingerprint] = model.CandidateResumePoint{CandidateID: ids[index], PrefixHash: candidatePrefixHash(ids[:index+1])}
	}
	r.resumePoints = points
	return nil
}

// readResumeCandidateIDs 复用已有卡片HTML解析和编号规则，不逐人读取五个字段，也不保存HTML。
func readResumeCandidateIDs(ctx context.Context, browser model.Browser, cfg model.Config) ([]string, error) {
	list, err := common.RequiredSelector(cfg, "candidate.list")
	if err != nil {
		return nil, err
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: list, Property: "html"})
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(result.Value))
	if err != nil {
		return nil, fmt.Errorf("核对候选人读取位置失败：%w", err)
	}
	ids := cardDOMIDs(doc)
	seen := make(map[string]bool, len(ids))
	for index, raw := range ids {
		id, err := stableCardID(raw, index)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, fmt.Errorf("前程无忧名单里有重复编号，这次先不继续，避免漏人或选错人")
		}
		seen[id] = true
		ids[index] = id
	}
	return ids, nil
}

// candidatePrefixHash 对包含最后完成者的编号前缀取摘要，追加新人不影响旧断点，换序或插入时安全从头。
func candidatePrefixHash(ids []string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(ids, "\n"))))
}
