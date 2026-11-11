// Package greeting 文件作用：只读找回上次未完成人选，交回原流程补处理，再恢复最新成功位置，不另建发送链路。
package greeting

import (
	"context"
	"fmt"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// retryCandidates 每次只读取一个已记录的未完成者；队列仅在本次启动取快照，补处理再次失败不在同一任务里循环重试。
func (s *candidateResumeSession) retryCandidates(ctx context.Context, browser model.Browser, cfg model.Config, runtime model.Runtime, options model.CandidateListOptions) ([]model.Candidate, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	s.retrying = false
	if s.retryOffset < len(s.retries) {
		if !s.restoreMain && s.record.Point.CandidateID != "" {
			anchor, found, err := s.readRecordedCandidate(ctx, browser, cfg, runtime, s.record.Point, options)
			if err != nil {
				return nil, false, err
			}
			if !found {
				return nil, false, s.restartFromHead(ctx, browser, cfg, options.Cursor)
			}
			s.highWaterIndex = anchor.Index
		}
		s.restoreMain = true
		point := s.retries[s.retryOffset]
		s.retryOffset++
		candidate, found, err := s.readRecordedCandidate(ctx, browser, cfg, runtime, point, options)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, s.restartFromHead(ctx, browser, cfg, options.Cursor)
		}
		s.retrying = true
		return []model.Candidate{candidate}, true, nil
	}
	if s.restoreMain {
		s.restoreMain = false
		found, err := s.runtime.RestoreCandidateList(ctx, browser, cfg, s.record.Point, options.Cursor)
		if err != nil {
			return nil, false, err
		}
		if !found && s.record.Point.CandidateID != "" {
			return nil, false, s.restartFromHead(ctx, browser, cfg, options.Cursor)
		}
	}
	return nil, false, nil
}

// readRecordedCandidate 兼容现有平台从锚点或下一人恢复的两种语义，最多往前只读一行，并用完整编号与前缀摘要核实，绝不按姓名猜测。
func (s *candidateResumeSession) readRecordedCandidate(ctx context.Context, browser model.Browser, cfg model.Config, runtime model.Runtime, point model.CandidateResumePoint, options model.CandidateListOptions) (model.Candidate, bool, error) {
	cursor := &model.CandidateListCursor{}
	found, err := s.runtime.RestoreCandidateList(ctx, browser, cfg, point, cursor)
	if err != nil || !found {
		return model.Candidate{}, false, err
	}
	single := cfg
	single.MaxItems = 1
	options.Cursor = cursor
	for attempt := 0; attempt < 2; attempt++ {
		rows, err := runtime.FindCandidates(ctx, browser, single, options)
		if common.IsElementMissing(err) {
			return model.Candidate{}, false, nil
		}
		if err != nil {
			return model.Candidate{}, false, fmt.Errorf("读取上次未完成的位置失败：%w", err)
		}
		if len(rows) == 1 {
			actual, ok := s.runtime.CandidateResumePoint(rows[0])
			if ok && actual == point {
				return rows[0], true, nil
			}
		}
		if cursor.StartIndex == 0 {
			break
		}
		cursor.StartIndex--
	}
	return model.Candidate{}, false, nil
}

// restartFromHead 在名单或前缀变化时清除不再可靠的定位并从头，不能把真实读取错误当作找不到。
func (s *candidateResumeSession) restartFromHead(ctx context.Context, browser model.Browser, cfg model.Config, cursor *model.CandidateListCursor) error {
	if _, err := s.runtime.RestoreCandidateList(ctx, browser, cfg, model.CandidateResumePoint{}, cursor); err != nil {
		return err
	}
	if err := s.save(ctx, candidateResumeRecord{Version: 2, ScopeHash: s.record.ScopeHash}); err != nil {
		return err
	}
	s.retries, s.retryOffset, s.retrying, s.restoreMain, s.highWaterIndex = nil, 0, false, false, -1
	return nil
}
