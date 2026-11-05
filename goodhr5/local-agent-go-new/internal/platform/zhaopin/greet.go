// Package zhaopin 文件作用：实现智联打招呼、收藏和不合适动作。
package zhaopin

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	greetingJobDialog    = "candidate.greet_job_dialog"
	greetingStateTimeout = 10 * time.Second
)

// GreetCandidate 向指定智联候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.GreetRequest) error {
	search, err := common.CurrentPageUsesManualSearch(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if !search {
		return common.GreetCandidate(ctx, browser, cfg, candidate, request)
	}
	if err = r.greetSearchCandidate(ctx, browser, cfg, candidate); err != nil && err != model.ErrCandidateAlreadyContacted {
		return fmt.Errorf("%w：%w", model.ErrGreetingUnconfirmed, err)
	}
	return err
}

// greetSearchCandidate 只点击一次当前卡片，按实际弹框选择沟通职位或发送招呼语，确认卡片更新后才记成功。
func (r *Runtime) greetSearchCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	for _, key := range []string{greetingJobDialog, "candidate.greet_dialog"} {
		opened, err := common.ProbeSelectorExists(ctx, browser, cfg, key)
		if err != nil {
			return err
		}
		if opened {
			return fmt.Errorf("页面上还有未处理的招呼弹框，请先关闭后再试")
		}
	}
	contacted, err := common.CandidateActionExists(ctx, browser, cfg, candidate, "candidate.continue")
	if err != nil {
		return err
	}
	if contacted {
		return model.ErrCandidateAlreadyContacted
	}
	if err := common.CandidateAction(ctx, browser, cfg, candidate, "candidate.greet"); err != nil {
		return err
	}

	confirmedJob, sentPhrase := false, false
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(greetingStateTimeout)
	for time.Now().Before(deadline) {
		jobRequired, err := common.ProbeSelectorExists(ctx, browser, cfg, greetingJobDialog)
		if err != nil {
			return err
		}
		if jobRequired {
			if !confirmedJob {
				if err := selectSearchGreetingJob(ctx, browser, cfg, r.positionName); err != nil {
					return err
				}
				confirmedJob = true
				deadline = time.Now().Add(greetingStateTimeout)
				continue
			}
		} else {
			phraseRequired, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_dialog")
			if err != nil {
				return err
			}
			if phraseRequired && !sentPhrase {
				if err := common.ClickRequired(ctx, browser, cfg, "candidate.greet_send"); err != nil {
					return err
				}
				sentPhrase = true
				deadline = time.Now().Add(greetingStateTimeout)
				continue
			}
			contacted, err := common.CandidateActionExists(ctx, browser, cfg, candidate, "candidate.continue")
			if err != nil {
				return err
			}
			if contacted && !phraseRequired {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return fmt.Errorf("智联卡片还没显示继续沟通，请先检查平台提示")
}

// selectSearchGreetingJob 在智联要求选择沟通职位时搜索任务岗位，只有唯一同名结果且回显一致才确认。
func selectSearchGreetingJob(ctx context.Context, browser model.Browser, cfg model.Config, name string) error {
	if name == "" {
		return fmt.Errorf("GoodHR 岗位名称为空，请先检查岗位设置")
	}
	if err := common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_open"); err != nil {
		return fmt.Errorf("打开沟通职位列表失败：%w", err)
	}
	if err := common.InputRequired(ctx, browser, cfg, "candidate.greet_job_input", common.PositionSearchQuery(name)); err != nil {
		return fmt.Errorf("搜索沟通职位失败：%w", err)
	}
	selector, err := common.RequiredSelector(cfg, "candidate.greet_job_name")
	if err != nil {
		return err
	}
	exact := true
	selector.Target.Text, selector.Target.ExactText = name, &exact
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 2, ExpectedMissing: true, ProbeOnly: true,
	})
	if err != nil && !common.IsElementMissing(err) {
		return fmt.Errorf("读取沟通职位结果失败：%w", err)
	}
	switch len(items) {
	case 0:
		return fmt.Errorf("智联没有找到同名职位“%s”，请核对 GoodHR 岗位名称", name)
	case 1:
	default:
		return fmt.Errorf("智联有多个职位叫“%s”，请先在智联手动选好沟通职位", name)
	}
	if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: selector}); err != nil {
		return fmt.Errorf("选择沟通职位失败：%w", err)
	}
	selected, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.greet_job_selected")
	if err != nil {
		return fmt.Errorf("核对已选沟通职位失败：%w", err)
	}
	if !found || selected != name {
		return fmt.Errorf("没确认智联选中了“%s”，请检查弹框中的沟通职位", name)
	}
	return common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_confirm")
}

// FavoriteCandidate 当前智联暂不支持自动收藏，直接跳过。
func (r *Runtime) FavoriteCandidate(context.Context, model.Browser, model.Config, model.Candidate) error {
	return nil
}

// RejectCandidate 将指定智联候选人标记为不合适。
func (r *Runtime) RejectCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.reject")
}
