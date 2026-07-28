// Package greeting 提供主动打招呼流程使用的可取消随机等待和模拟休息计划。
package greeting

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// restSchedule 保存一次任务内的模拟休息进度。
type restSchedule struct {
	maxTimes       int
	usedTimes      int
	nextAfter      int
	processedSince int
}

// newRestSchedule 根据个人配置生成本次任务的休息次数和首次触发点。
func newRestSchedule(preferences cloud.UserPreferences) restSchedule {
	if preferences.RestDurationMax <= 0 {
		return restSchedule{}
	}
	return restSchedule{
		maxTimes:  randomIntRange(preferences.RestTimesMin, preferences.RestTimesMax),
		nextAfter: randomIntRange(preferences.RestAfterCandidatesMin, preferences.RestAfterCandidatesMax),
	}
}

// waitRandomSeconds 在指定秒数范围内随机等待，并响应任务取消。
func waitRandomSeconds(ctx context.Context, logger shared.Logger, taskID string, label string, minimum float64, maximum float64) error {
	if maximum < minimum {
		maximum = minimum
	}
	if maximum <= 0 {
		return nil
	}
	seconds := minimum
	if maximum > minimum {
		seconds += rand.Float64() * (maximum - minimum)
	}
	if seconds <= 0 {
		return nil
	}
	startedAt := time.Now()
	if logger != nil {
		logger.Step(taskID, "greeting", "wait_"+label, "start", startedAt, nil)
	}
	timer := time.NewTimer(time.Duration(seconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		if logger != nil {
			logger.Step(taskID, "greeting", "wait_"+label, "failed", startedAt, ctx.Err())
		}
		return ctx.Err()
	case <-timer.C:
		if logger != nil {
			logger.Step(taskID, "greeting", "wait_"+label, "success", startedAt, nil)
		}
		return nil
	}
}

// afterCandidate 在处理完候选人后按计划同步显示浮层并等待休息结束。
func (schedule *restSchedule) afterCandidate(ctx context.Context, browser *client.Client, logger shared.Logger, taskID string, preferences cloud.UserPreferences) error {
	if schedule == nil || schedule.maxTimes <= 0 || schedule.usedTimes >= schedule.maxTimes || schedule.nextAfter <= 0 {
		return nil
	}
	schedule.processedSince++
	if schedule.processedSince < schedule.nextAfter {
		return nil
	}
	durationMinutes := randomFloatRange(preferences.RestDurationMin, preferences.RestDurationMax)
	if durationMinutes <= 0 {
		return nil
	}
	schedule.usedTimes++
	schedule.processedSince = 0
	schedule.nextAfter = randomIntRange(preferences.RestAfterCandidatesMin, preferences.RestAfterCandidatesMax)
	duration := time.Duration(durationMinutes * float64(time.Minute))
	endsAt := time.Now().Add(duration)
	overlayID := "goodhr-simulated-rest"
	message := fmt.Sprintf("我先安静休息一会儿，预计 %s 继续干活", endsAt.Format("15:04:05"))
	if _, err := browser.ShowOverlay(ctx, contract.OverlayShowRequest{
		OverlayID: overlayID,
		Title:     "模拟休息中",
		Subtitle:  fmt.Sprintf("第 %d 次休息", schedule.usedTimes),
		Message:   message,
		Level:     "info",
		MaxAgeMS:  int(duration.Milliseconds()) + 60_000,
	}); err != nil && logger != nil {
		logger.Step(taskID, "greeting", "show_rest_overlay", "warning", time.Now(), err)
	}
	waitErr := waitDuration(ctx, duration)
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := browser.CloseOverlay(closeCtx, contract.OverlayCloseRequest{OverlayID: overlayID}); err != nil && logger != nil {
		logger.Step(taskID, "greeting", "close_rest_overlay", "warning", time.Now(), err)
	}
	return waitErr
}

// waitDuration 等待指定时长并响应任务取消。
func waitDuration(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// randomIntRange 返回闭区间内的随机整数。
func randomIntRange(minimum int, maximum int) int {
	if maximum <= minimum {
		return minimum
	}
	return minimum + rand.IntN(maximum-minimum+1)
}

// randomFloatRange 返回闭区间内的随机浮点数。
func randomFloatRange(minimum float64, maximum float64) float64 {
	if maximum <= minimum {
		return minimum
	}
	return minimum + rand.Float64()*(maximum-minimum)
}
