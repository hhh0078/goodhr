// Package greeting 文件作用：复用本地设置分别保存最新完成位置与未完成人选，按用户、账号、岗位和页面恢复跨任务进度。
package greeting

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// candidateResumeRecord 是 local_settings 中的版本化断点，不保存页面地址、姓名和正文。
type candidateResumeRecord struct {
	Version   int                          `json:"version"`
	ScopeHash string                       `json:"scope_hash"`
	Point     model.CandidateResumePoint   `json:"point"`
	Pending   []model.CandidateResumePoint `json:"pending,omitempty"`
}

// candidateResumeSession 保留未完成者并独立推进最新成功位置，重启先补处理，再回到最新位置；不保存人名或正文。
type candidateResumeSession struct {
	store          *storage.Store
	runtime        model.CandidateListResumer
	key            string
	record         candidateResumeRecord
	retries        []model.CandidateResumePoint
	retryOffset    int
	retrying       bool
	restoreMain    bool
	highWaterIndex int
}

// prepareCandidateResume 读取上次进度并让平台只读核对编号；没有可靠断点能力的平台保持原流程。
func (f *Flow) prepareCandidateResume(ctx context.Context, browser model.Browser, prepared shared.PreparedTask, runtime model.Runtime, cursor *model.CandidateListCursor) (*candidateResumeSession, error) {
	resumer, supported := runtime.(model.CandidateListResumer)
	if !supported || f.Store == nil || prepared.Session.UserID == "" || prepared.Position.ID == "" || prepared.ProfilePath == "" {
		return nil, nil
	}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("核对进度所在页面失败：%w", err)
	}
	pageURL, currentCount := "", 0
	for _, page := range pages.Pages {
		if page.Current {
			pageURL = strings.TrimSpace(page.URL)
			currentCount++
		}
	}
	if currentCount != 1 || pageURL == "" {
		return nil, fmt.Errorf("没确认当前人才页面，暂不恢复进度，请切到目标页面后再试")
	}
	key, scope, err := candidateResumeScope(prepared, pageURL)
	if err != nil {
		return nil, err
	}
	raw, err := f.Store.GetSetting(ctx, key)
	if err != nil {
		return nil, err
	}
	var record candidateResumeRecord
	if json.Unmarshal([]byte(raw), &record) != nil || (record.Version != 1 && record.Version != 2) || record.ScopeHash != scope {
		record = candidateResumeRecord{Version: 2, ScopeHash: scope}
	}
	record.Version = 2 // 旧客户端不认识第二版时会从头检查，不能忽略未完成者直接跳到新位置。
	found, err := resumer.RestoreCandidateList(ctx, browser, prepared.Platform, record.Point, cursor)
	if err != nil {
		return nil, fmt.Errorf("查找上次候选人位置失败：%w", err)
	}
	if found {
		shared.ReportProgress(f.Logger, prepared.Request.TaskID, "找到上次处理的位置了，我接着往下看")
	} else if record.Point.CandidateID != "" {
		record = candidateResumeRecord{Version: 2, ScopeHash: scope}
		if raw != "" {
			shared.ReportProgress(f.Logger, prepared.Request.TaskID, "上次的位置没对上，这次从头分批检查")
		}
	}
	return &candidateResumeSession{store: f.Store, runtime: resumer, key: key, record: record,
		retries: append([]model.CandidateResumePoint(nil), record.Pending...), highWaterIndex: -1}, nil
}

// candidateResumeScope 把绑定信息和影响判断的岗位配置分别取摘要，不将Token、URL或个人信息写进设置键。
func candidateResumeScope(prepared shared.PreparedTask, pageURL string) (string, string, error) {
	identity, err := json.Marshal([]string{prepared.Session.UserID, prepared.Platform.ID, prepared.Position.ID,
		prepared.ProfilePath, prepared.Request.ProfileID, prepared.Request.PlatformAccountID})
	if err != nil {
		return "", "", err
	}
	position := prepared.Position
	scope, err := json.Marshal(struct {
		PageURL         string                     `json:"page_url"`
		Name            string                     `json:"name"`
		Keyword         string                     `json:"keyword"`
		Keywords        []string                   `json:"keywords"`
		ExcludeKeywords []string                   `json:"exclude_keywords"`
		IsAndMode       bool                       `json:"is_and_mode"`
		Description     string                     `json:"description"`
		CommonConfig    cloud.PositionCommonConfig `json:"common_config"`
		AIOptions       cloud.PositionAIOptions    `json:"ai_options"`
		EnableGreet     *bool                      `json:"enable_greet"`
		RequestPhone    bool                       `json:"request_phone"`
		RequestWechat   bool                       `json:"request_wechat"`
		RequestResume   bool                       `json:"request_resume"`
	}{pageURL, position.Name, position.Keyword, position.Keywords, position.ExcludeKeywords, position.IsAndMode,
		position.Description, position.CommonConfig, position.AIOptions, prepared.Request.EnableGreet,
		position.RequestPhone, position.RequestWechat, position.RequestResume})
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("greeting_resume:%x", sha256.Sum256(identity)), fmt.Sprintf("%x", sha256.Sum256(scope)), nil
}

// completedAnchor 仅跳过前缀和联合身份都吻合的重叠锚点，不把同名同龄但经历不同的人一起跳过。
func (s *candidateResumeSession) completedAnchor(candidate model.Candidate) bool {
	if s == nil || s.retrying || s.record.Point.CandidateID == "" {
		return false
	}
	point, ok := s.runtime.CandidateResumePoint(candidate)
	return ok && point == s.record.Point
}

// complete 同时持久化最新成功位置与未完成者；补处理不能把较新的位置倒退，失败者也不能被后续成功覆盖。
func (s *candidateResumeSession) complete(ctx context.Context, candidate model.Candidate, candidateErr error) error {
	if s == nil {
		return nil
	}
	point, ok := s.runtime.CandidateResumePoint(candidate)
	if !ok || point.CandidateID == "" || point.PrefixHash == "" || candidate.Fingerprint == "" {
		return fmt.Errorf("这位候选人的进度没确认，先停一下，避免后面无法接着扫描")
	}
	record := s.record
	record.Pending = nil
	for _, pending := range s.record.Pending {
		if pending != point {
			record.Pending = append(record.Pending, pending)
		}
	}
	if candidateErr != nil {
		record.Pending = append(record.Pending, point)
	} else if !s.retrying || s.record.Point.CandidateID == "" || candidate.Index > s.highWaterIndex {
		record.Point = point
	}
	if err := s.save(ctx, record); err != nil {
		return err
	}
	if record.Point == point && candidateErr == nil {
		s.highWaterIndex = candidate.Index
	}
	return nil
}

// save 先完成设置写入再更新内存，取消或写入失败不得假装进度已保存。
func (s *candidateResumeSession) save(ctx context.Context, record candidateResumeRecord) error {
	record.Version = 2
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.store.SetSetting(writeCtx, s.key, string(raw)); err != nil {
		return fmt.Errorf("保存候选人进度失败，先停一下，避免跳过还没处理的人：%w", err)
	}
	s.record = record
	return nil
}
