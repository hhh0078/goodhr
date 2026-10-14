// Package greeting 文件作用：验证收藏只取详情 AI 分数、独立于招呼门槛，并在失败或超时后安全关闭详情。
package greeting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// favoriteRuntime 记录收藏和关闭顺序；不支持收藏时模拟平台直接返回的空操作。
type favoriteRuntime struct {
	greetingSettingRuntime
	favorites, favoriteClicks int
	unsupported               bool
	favoriteErr               error
	onFavorite                func(context.Context) error
	order                     []string
	cleanupContexts           []context.Context
	cleanupErrors             []error
}

// FavoriteCandidate 只允许在当前详情打开且尚未关闭时收藏，按场景模拟收藏失败和空操作。
func (r *favoriteRuntime) FavoriteCandidate(ctx context.Context, _ model.Browser, _ model.Config, _ model.Candidate) error {
	r.favorites++
	r.order = append(r.order, "favorite")
	if r.opens != 1 || r.closes != 0 {
		return errors.New("收藏必须在当前详情关闭前执行")
	}
	if r.onFavorite != nil {
		return r.onFavorite(ctx)
	}
	if r.favoriteErr != nil {
		return r.favoriteErr
	}
	if !r.unsupported {
		r.favoriteClicks++
	}
	return nil
}

// CloseCandidateDetail 记录详情关闭顺序和收到的清理上下文。
func (r *favoriteRuntime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	r.order = append(r.order, "close")
	r.cleanupContexts = append(r.cleanupContexts, ctx)
	r.cleanupErrors = append(r.cleanupErrors, ctx.Err())
	return r.greetingSettingRuntime.CloseCandidateDetail(ctx, browser, cfg, candidate)
}

// favoriteLogger 保存收藏步骤的最终状态，防止失败被记为成功。
type favoriteLogger struct{ statuses []string }

// Step 只收集收藏步骤的开始、成功和失败。
func (l *favoriteLogger) Step(_ string, _ string, step string, status string, _ time.Time, _ error) {
	if step == "favorite_candidate" {
		l.statuses = append(l.statuses, status)
	}
}

// newFavoriteTestFlow 复用真实 AI 客户端和内存存储，使用固定评分服务提供详情 AI 结果。
func newFavoriteTestFlow(t *testing.T, score float64) (Flow, shared.PreparedTask) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content, _ := json.Marshal(ai.Decision{Score: score, Reason: "符合测试要求"})
		encodedContent, _ := json.Marshal(string(content))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + string(encodedContent) + `}}]}`))
	}))
	t.Cleanup(server.Close)
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return Flow{Browser: client.New(server.URL), AI: ai.New(), Store: store}, shared.PreparedTask{
		Request: shared.StartRequest{TaskID: "favorite-test", TaskType: "greeting"},
		Position: cloud.PositionSnapshot{
			RequiresAI:   true,
			CommonConfig: cloud.PositionCommonConfig{ModeDefault: "ai", DetailMode: "dom"},
			AI:           cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture", ScoreThreshold: 1},
			AIOptions:    cloud.PositionAIOptions{FavoriteScoreThreshold: 80},
		},
	}
}

// TestFavoriteThresholdAndGreetingIndependence 验证默认关闭、严格分数边界、只检查模式及不支持收藏的平台。
func TestFavoriteThresholdAndGreetingIndependence(t *testing.T) {
	for _, test := range []struct {
		name                         string
		score, threshold, greetScore float64
		disabled, noAI, keyword      bool
		unsupported                  bool
		favorites, clicks, greetings int
	}{
		{name: "default_disabled", score: 99, threshold: 0, greetings: 1},
		{name: "negative_disabled", score: 99, threshold: -1, greetings: 1},
		{name: "equal", score: 80, threshold: 80, greetings: 1},
		{name: "below", score: 79, threshold: 80, greetings: 1},
		{name: "above", score: 81, threshold: 80, favorites: 1, clicks: 1, greetings: 1},
		{name: "greeting_disabled", score: 81, threshold: 80, disabled: true, favorites: 1, clicks: 1},
		{name: "below_greet_threshold", score: 81, threshold: 80, greetScore: 90, favorites: 1, clicks: 1},
		{name: "no_detail_score", score: 99, threshold: 80, noAI: true, greetings: 1},
		{name: "keyword_even_with_legacy_ai", score: 99, threshold: 80, keyword: true, greetings: 1},
		{name: "unsupported_platform", score: 81, threshold: 80, unsupported: true, favorites: 1, greetings: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			flow, prepared := newFavoriteTestFlow(t, test.score)
			prepared.Position.AIOptions.FavoriteScoreThreshold = test.threshold
			prepared.Position.AIOptions.GreetScoreThreshold = test.greetScore
			prepared.Position.RequiresAI = !test.noAI
			if test.keyword {
				prepared.Position.CommonConfig.ModeDefault = "keyword"
				prepared.Preferences.DetailOpenProbability = 100
			}
			if test.disabled {
				enabled := false
				prepared.Request.EnableGreet = &enabled
				prepared.Preferences.GreetBeforeDelayMin = 1
				prepared.Preferences.GreetBeforeDelayMax = 1
			}
			runtime := &favoriteRuntime{unsupported: test.unsupported}
			stats := shared.Stats{}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			if err := flow.processCandidate(ctx, prepared, runtime, model.Candidate{Fingerprint: "fixture"}, &stats); err != nil {
				t.Fatal(err)
			}
			if runtime.favorites != test.favorites || runtime.favoriteClicks != test.clicks || runtime.greetings != test.greetings || runtime.closes != 1 {
				t.Fatalf("收藏或招呼规则错误：收藏调用=%d 点击=%d 招呼=%d 关闭=%d", runtime.favorites, runtime.favoriteClicks, runtime.greetings, runtime.closes)
			}
			wantOrder := []string{"close"}
			if test.favorites == 1 {
				wantOrder = []string{"favorite", "close"}
			}
			if !reflect.DeepEqual(runtime.order, wantOrder) || stats.Succeeded != test.greetings || stats.Failed != 0 {
				t.Fatalf("收藏必须在关闭前且不能计为招呼成功：顺序=%v 统计=%+v", runtime.order, stats)
			}
		})
	}
}

// TestFavoriteFailureStillClosesDetail 验证收藏错误原样传播、失败不会伪成功，取消与超时也保留清理预算。
func TestFavoriteFailureStillClosesDetail(t *testing.T) {
	for _, stage := range []string{"operation", "fatal", "cancel", "timeout", "cleanup_fatal", "cleanup_unconfirmed"} {
		t.Run(stage, func(t *testing.T) {
			flow, prepared := newFavoriteTestFlow(t, 90)
			logger := &favoriteLogger{}
			flow.Logger = logger
			runtime := &favoriteRuntime{favoriteErr: errors.New("合成收藏失败")}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			wantErr := runtime.favoriteErr
			switch stage {
			case "fatal":
				runtime.favoriteErr = model.ErrCandidateDetailCleanupFailed
				wantErr = runtime.favoriteErr
			case "cancel":
				runtime.onFavorite = func(callCtx context.Context) error { cancel(); return callCtx.Err() }
				wantErr = context.Canceled
			case "timeout":
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				runtime.onFavorite = func(callCtx context.Context) error { <-callCtx.Done(); return callCtx.Err() }
				wantErr = context.DeadlineExceeded
			case "cleanup_fatal":
				runtime.closeErr = model.ErrCandidateDetailCleanupFailed
			case "cleanup_unconfirmed":
				runtime.closeErr = errors.New("合成详情仍未关闭")
			}
			stats := shared.Stats{}
			err := flow.processCandidate(ctx, prepared, runtime, model.Candidate{Fingerprint: "fixture"}, &stats)
			if !errors.Is(err, wantErr) || runtime.closes != 1 || runtime.greetings != 0 || stats.Failed != 1 {
				t.Fatalf("收藏失败应保留错误并关闭详情：err=%v 关闭=%d 招呼=%d 统计=%+v", err, runtime.closes, runtime.greetings, stats)
			}
			if (stage == "fatal" || stage == "cancel" || stage == "cleanup_fatal" || stage == "cleanup_unconfirmed") && !shared.ShouldStopTaskImmediately(err) {
				t.Fatalf("致命错误不能被吞掉：%v", err)
			}
			if (stage == "cleanup_fatal" || stage == "cleanup_unconfirmed") && !errors.Is(err, runtime.closeErr) {
				t.Fatalf("收藏与关闭两个错误都必须保留：%v", err)
			}
			if !reflect.DeepEqual(logger.statuses, []string{"start", "failed"}) {
				t.Fatalf("收藏失败不能记成功：%v", logger.statuses)
			}
			deadline, hasDeadline := runtime.cleanupContexts[0].Deadline()
			remaining := time.Until(deadline)
			if runtime.cleanupErrors[0] != nil || !hasDeadline || remaining <= 40*time.Second || remaining > 45*time.Second {
				t.Fatalf("详情补关需要独立且有限的预算：hasDeadline=%v remaining=%v", hasDeadline, remaining)
			}
		})
	}
}

// TestFavoriteSuccessThenCloseDelayTimeout 验证收藏后关闭等待超时仍补关，不重复收藏或发送招呼。
func TestFavoriteSuccessThenCloseDelayTimeout(t *testing.T) {
	flow, prepared := newFavoriteTestFlow(t, 90)
	prepared.Preferences.DetailCloseDelayMin, prepared.Preferences.DetailCloseDelayMax = 1, 1
	runtime := &favoriteRuntime{}
	stats := shared.Stats{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := flow.processCandidate(ctx, prepared, runtime, model.Candidate{Fingerprint: "fixture"}, &stats); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("收藏后等待应遵守候选人预算：%v", err)
	}
	if runtime.favorites != 1 || runtime.closes != 1 || runtime.greetings != 0 {
		t.Fatalf("等待超时仍补关且不重复动作：收藏=%d 关闭=%d 招呼=%d", runtime.favorites, runtime.closes, runtime.greetings)
	}
}
