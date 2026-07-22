// Package hliepin 本文件负责组装猎聘专用父子唯一选择器，并调用位置稳定后只点击一次的 Worker 动作。
package hliepin

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"goodhr5/local-agent-go/internal/platformcore"
)

const (
	hliepinStableClickPath       = "/api/v1/hliepin/stable-click"
	hliepinGreetModalParent      = `.ant-modal:has(.ant-form-item [id="jobId"])`
	hliepinGreetJobSelectTarget  = `.ant-form-item:has([id="jobId"]) .ant-select-selector`
	hliepinGreetDropdownParent   = `.ant-select-dropdown:not(.ant-select-dropdown-hidden):has(.hpublic-job-select-option)`
	hliepinGreetJobOptionTarget  = `.ant-select-item-option`
	hliepinGreetJobOptionNested  = `.hpublic-job-select-option`
	hliepinGreetSubmitTarget     = `.xpath-job-and-msg-ok-btn-new`
	hliepinGreetWithoutJobTarget = `.directly-open-chat-btn`
	hliepinCandidateButtonTarget = `.card-bottom-right button.lp-ant-btn-light`
	hliepinChatModalParent       = `.im-ui-basic-chat-modal`
	hliepinCandidateDrawerParent = `#im-basic-contact`
)

var hliepinStableCandidateIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// hliepinCandidateRowParentSelector 使用稳定简历 ID 生成唯一候选人行父级，禁止退回全局按钮选择器。
func hliepinCandidateRowParentSelector(candidate platformcore.Candidate) (string, error) {
	candidateID := strings.TrimSpace(stringFromMap(candidate, "platform_candidate_id"))
	if candidateID == "" || !hliepinStableCandidateIDPattern.MatchString(candidateID) {
		return "", fmt.Errorf("猎聘候选人%s缺少可安全定位的简历 ID", candidateName(candidate))
	}
	return "tbody tr.r-" + candidateID, nil
}

// hliepinStableClick 在唯一父级中定位唯一目标，等待位置稳定并保证只执行一次点击。
func hliepinStableClick(ctx context.Context, exec platformcore.Executor, parentSelector string, targetSelector string, options map[string]any) (map[string]any, error) {
	payload := map[string]any{
		"parent_selector":    parentSelector,
		"target_selector":    targetSelector,
		"stable_checks":      3,
		"stability_interval": 120,
		"stability_timeout":  5000,
		"position_tolerance": 2,
		"max_move_attempts":  3,
		"timeout":            5000,
	}
	for key, value := range options {
		payload[key] = value
	}
	return exec.Post(ctx, hliepinStableClickPath, payload)
}
