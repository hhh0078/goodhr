// Package common 文件作用：推进滚动加载列表，确认真实列表尾部并保留跨批读取位置。
package common

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	candidateScrollMaxAttempts  = 120
	candidateBottomStableRounds = 3
)

// candidateWindowState 表示下一批读取结果和当前列表是否已经能够稳定读取。
type candidateWindowState struct {
	changed    bool
	ready      bool
	startIndex int
}

// advanceCandidateScroll 持续用真实滚轮抵达已加载列表末尾，再等待追加内容；只在确认底部稳定后结束。
func advanceCandidateScroll(ctx context.Context, browser model.Browser, cfg model.Config, platformID string, before []model.Candidate, options model.CandidateListOptions) (bool, error) {
	if len(before) == 0 {
		return false, fmt.Errorf("候选人列表暂时为空，无法确认是否已经到底")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	anchor, err := RequiredSelector(cfg, "candidate.list")
	if err != nil {
		return false, err
	}
	nextIndex := 0
	last := before[0]
	for _, candidate := range before {
		if candidate.Index >= last.Index {
			last = candidate
		}
		nextIndex = max(nextIndex, candidate.Index+1)
	}
	tail, err := IndexedSelector(cfg, "candidate.item", last.Index)
	if err != nil {
		return false, err
	}
	tail.Description = cfg.Name + "已加载列表末尾"
	headBefore := before[:1]
	if options.Cursor != nil && options.Cursor.StartIndex > 0 {
		headCfg := cfg
		headCfg.MaxItems = 1
		headOptions := options
		headOptions.Cursor = &model.CandidateListCursor{}
		headBefore, err = FindCandidates(ctx, browser, headCfg, platformID, headOptions)
		if err != nil {
			return false, err
		}
	}
	if len(headBefore) == 0 {
		return false, fmt.Errorf("候选人列表正在刷新，暂时无法定位列表开头")
	}
	headSet := candidateFingerprintSet(headBefore)
	bottomRounds, blockedRounds := 0, 0
	for attempt := 0; attempt < candidateScrollMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("等待下一批候选人未完成：%w", err)
		}
		state, err := readNextCandidateWindow(ctx, browser, cfg, platformID, options, nextIndex, headSet)
		if err != nil {
			return false, err
		}
		if state.changed {
			advanceCandidateCursor(options.Cursor, state.startIndex, before)
			return true, nil
		}
		result, err := browser.Scroll(ctx, contract.ScrollRequest{
			WheelAnchor: &anchor, ProgressTarget: &tail,
			Distance: positiveOr(cfg.ScrollDistance, 620), MaxAttempts: 1,
			WaitMS: int(candidateListPollInterval / time.Millisecond),
		})
		if err != nil {
			return false, fmt.Errorf("加载更多候选人时滚动失败：%w", err)
		}
		if result.ProgressTargetVisible == nil || result.ProgressTargetMoved == nil {
			return false, fmt.Errorf("浏览器工具没有返回列表末尾的位置，无法确认到底，请更新本地程序后重试")
		}
		state, err = readNextCandidateWindow(ctx, browser, cfg, platformID, options, nextIndex, headSet)
		if err != nil {
			return false, err
		}
		if state.changed {
			advanceCandidateCursor(options.Cursor, state.startIndex, before)
			return true, nil
		}
		if *result.ProgressTargetMoved {
			bottomRounds, blockedRounds = 0, 0
			continue
		}
		if !*result.ProgressTargetVisible {
			bottomRounds = 0
			blockedRounds++
			if blockedRounds >= candidateBottomStableRounds {
				return false, fmt.Errorf("列表末尾还没进入窗口，真实滚轮却没有推动列表，暂不把它当成任务结束")
			}
			continue
		}
		// 已加载尾部可见且真实向下滚动没有位移，才给网络加载留出完整等待时间。
		for poll := 1; poll < candidateListPollAttempts; poll++ {
			timer := time.NewTimer(candidateListPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, ctx.Err()
			case <-timer.C:
			}
			state, err = readNextCandidateWindow(ctx, browser, cfg, platformID, options, nextIndex, headSet)
			if err != nil {
				return false, err
			}
			if state.changed {
				advanceCandidateCursor(options.Cursor, state.startIndex, before)
				return true, nil
			}
		}
		if state.ready {
			bottomRounds++
		} else {
			bottomRounds = 0
		}
		if bottomRounds >= candidateBottomStableRounds {
			return false, nil
		}
	}
	return false, fmt.Errorf("滚动加载仍未确认到底或出现下一批候选人，已记录当前步骤，请稍后重试")
}

// advanceCandidateCursor 在追加窗口时累计身份顺序，虚拟列表替换时清空旧行的定位信息。
func advanceCandidateCursor(cursor *model.CandidateListCursor, startIndex int, before []model.Candidate) {
	if cursor == nil {
		return
	}
	if startIndex == 0 {
		cursor.IdentityOccurrences = nil
	} else {
		if cursor.IdentityOccurrences == nil {
			cursor.IdentityOccurrences = make(map[string]int)
		}
		for _, candidate := range before {
			identity := strings.Join(candidate.IdentityTexts, "\x00")
			cursor.IdentityOccurrences[identity]++
		}
	}
	cursor.StartIndex = startIndex
}

// readNextCandidateWindow 读取下一批追加内容，并识别虚拟列表替换旧行后需要从零重新读取的情况。
func readNextCandidateWindow(ctx context.Context, browser model.Browser, cfg model.Config, platformID string, options model.CandidateListOptions, nextIndex int, headBefore map[string]struct{}) (candidateWindowState, error) {
	headCfg := cfg
	headCfg.MaxItems = 1
	headOptions := options
	headOptions.Cursor = &model.CandidateListCursor{}
	head, err := FindCandidates(ctx, browser, headCfg, platformID, headOptions)
	if IsElementMissing(err) {
		return candidateWindowState{}, nil
	}
	if err != nil {
		return candidateWindowState{}, err
	}
	if len(head) == 0 {
		return candidateWindowState{}, nil
	}
	for fingerprint := range candidateFingerprintSet(head) {
		if _, unchanged := headBefore[fingerprint]; !unchanged {
			return candidateWindowState{changed: true, ready: true, startIndex: 0}, nil
		}
	}
	nextOptions := options
	nextOptions.Cursor = &model.CandidateListCursor{StartIndex: nextIndex}
	next, err := FindCandidates(ctx, browser, cfg, platformID, nextOptions)
	if IsElementMissing(err) {
		return candidateWindowState{}, nil
	}
	if err != nil {
		return candidateWindowState{}, err
	}
	return candidateWindowState{changed: len(next) > 0, ready: true, startIndex: nextIndex}, nil
}
