// Package greeting 提供主动打招呼流程使用的可取消随机等待和模拟休息计划。
package greeting

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

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
		shared.ReportProgress(
			logger,
			taskID,
			fmt.Sprintf("按个人设置，%s停留 %.1f 秒", waitLabel(label), seconds),
		)
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

// afterCandidate 在处理完候选人后按计划等待休息结束，不修改招聘页面。
func (schedule *restSchedule) afterCandidate(ctx context.Context, logger shared.Logger, taskID string, preferences cloud.UserPreferences) error {
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
	startedAt := time.Now()
	if logger != nil {
		logger.Step(taskID, "greeting", "simulated_rest", "start", startedAt, nil)
		shared.ReportProgress(
			logger,
			taskID,
			fmt.Sprintf(
				"按个人设置模拟休息 %s，预计 %s 继续",
				formatWaitDuration(duration),
				time.Now().Add(duration).Format("15:04:05"),
			),
		)
	}
	waitErr := waitDuration(ctx, duration)
	if logger != nil {
		status := "success"
		if waitErr != nil {
			status = "failed"
		}
		logger.Step(taskID, "greeting", "simulated_rest", status, startedAt, waitErr)
	}
	return waitErr
}

// waitLabel 返回随机等待场景对应的用户可见中文说明。
func waitLabel(label string) string {
	labels := map[string]string{
		"list_view":           "浏览候选人列表前",
		"after_scroll":        "等待下一批候选人加载",
		"before_detail_open":  "准备打开详情前",
		"detail_view":         "浏览候选人详情",
		"before_detail_close": "准备关闭详情前",
		"before_greet":        "准备打招呼前",
	}
	if value := labels[label]; value != "" {
		return value
	}
	return "继续下一步前"
}

// formatWaitDuration 把休息时长整理成分钟和秒的短中文。
func formatWaitDuration(duration time.Duration) string {
	duration = duration.Round(time.Second)
	minutes := int(duration / time.Minute)
	seconds := int((duration % time.Minute) / time.Second)
	if minutes <= 0 {
		return fmt.Sprintf("%d 秒", max(seconds, 1))
	}
	if seconds <= 0 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	return fmt.Sprintf("%d 分 %d 秒", minutes, seconds)
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
