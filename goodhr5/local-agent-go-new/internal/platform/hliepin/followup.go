// Package hliepin 文件作用：实现猎聘猎头端打招呼后的电话、微信、简历和追加消息动作。
package hliepin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// RequestCandidateInfo 复用或打开当前候选人聊天框，校验姓名后索要信息并统一收尾。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.CandidateInfoRequest) (resultErr error) {
	if !request.RequestPhone && !request.RequestWechat && !request.RequestResume && strings.TrimSpace(request.Message) == "" {
		return nil
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		if err := closeCandidatePanels(cleanupCtx, browser, cfg); err != nil && resultErr == nil {
			resultErr = err
		}
	}()
	reused, err := reusableCandidateChat(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	if !reused {
		if err = closeCandidatePanels(ctx, browser, cfg); err != nil {
			return err
		}
		if err = common.CandidateAction(ctx, browser, cfg, candidate, "candidate.continue"); err != nil {
			return fmt.Errorf("点击猎聘“继续沟通”失败：%w", err)
		}
		if err = verifyCandidateChat(ctx, browser, cfg, candidate); err != nil {
			return err
		}
	}
	if err = common.RequestCandidateInfo(ctx, browser, cfg, request); err != nil {
		return fmt.Errorf("猎聘索要候选人信息失败：%w", err)
	}
	return nil
}

// reusableCandidateChat 判断当前聊天框是否属于正在处理的候选人。
func reusableCandidateChat(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (bool, error) {
	exists, err := common.SelectorExists(ctx, browser, cfg, "candidate.chat_modal")
	if err != nil || !exists {
		return false, err
	}
	name, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.chat_name")
	if err != nil {
		return false, err
	}
	return found && candidateNamesMatch(candidate.Name, name), nil
}

// verifyCandidateChat 确认打开的聊天框属于当前候选人，避免跨候选人串台。
func verifyCandidateChat(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	name, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.chat_name")
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("猎聘聊天框已打开，但没有读取到候选人姓名")
	}
	if !candidateNamesMatch(candidate.Name, name) {
		return fmt.Errorf("猎聘聊天候选人不匹配：预期=%s，实际=%s", candidate.Name, name)
	}
	return nil
}

// candidateNamesMatch 比较完整或脱敏的猎聘候选人姓名。
func candidateNamesMatch(expected string, actual string) bool {
	normalize := func(value string) string {
		value = strings.ReplaceAll(strings.TrimSpace(value), " ", "")
		value = strings.TrimSuffix(value, "先生")
		return strings.TrimSuffix(value, "女士")
	}
	expected = normalize(expected)
	actual = normalize(actual)
	if expected == "" || actual == "" {
		return false
	}
	if strings.Contains(expected, "*") {
		return []rune(expected)[0] == []rune(actual)[0]
	}
	return expected == actual
}
