// Package lifecycle 文件作用：验证平台公开方法的成功、失败和 panic 都进入岗位日志。
package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type runtimeLogFixture struct {
	model.Runtime
	failure error
	panics  bool
}

// PlatformID 返回测试平台编号。
func (f runtimeLogFixture) PlatformID() string { return "test" }

// OpenLoginPage 模拟平台方法失败或 panic。
func (f runtimeLogFixture) OpenLoginPage(context.Context, model.Browser, model.Config) error {
	if f.panics {
		panic("test panic")
	}
	return f.failure
}

type runtimeLogCollector struct {
	events []runtimeLogEvent
	errors []error
}
type runtimeLogEvent struct{ step, status string }

// Step 收集测试方法的最终日志。
func (c *runtimeLogCollector) Step(_ string, _ string, step string, status string, _ time.Time, err error) {
	c.events = append(c.events, runtimeLogEvent{step, status})
	c.errors = append(c.errors, err)
}

// TestLoggedRuntimeRecordsMethods 验证正常调用与失败调用都至少有一条结果日志。
func TestLoggedRuntimeRecordsMethods(t *testing.T) {
	collector := &runtimeLogCollector{}
	runtime := newLoggedRuntime(runtimeLogFixture{failure: errors.New("页面未打开")}, "task", collector)
	if runtime.PlatformID() != "test" {
		t.Fatal("平台编号未返回")
	}
	if err := runtime.OpenLoginPage(context.Background(), nil, model.Config{}); err == nil {
		t.Fatal("平台失败没有返回")
	}
	if len(collector.events) != 2 || collector.events[0].status != "success" || collector.events[1] != (runtimeLogEvent{"open_login_page", "failed"}) {
		t.Fatalf("方法日志不完整：%+v", collector.events)
	}
}

// TestLoggedRuntimeRecordsPanic 验证平台 panic 写失败日志并继续由任务边界处理。
func TestLoggedRuntimeRecordsPanic(t *testing.T) {
	collector := &runtimeLogCollector{}
	runtime := newLoggedRuntime(runtimeLogFixture{panics: true}, "task", collector)
	defer func() {
		if recover() == nil {
			t.Fatal("平台 panic 被意外吞掉")
		}
		if len(collector.events) != 1 || collector.events[0].status != "panic" {
			t.Fatalf("平台 panic 没有日志：%+v", collector.events)
		}
	}()
	_ = runtime.OpenLoginPage(context.Background(), nil, model.Config{})
}

// detailScrollLogBrowser 记录各平台详情浏览是否真正调用了浏览器滚轮能力。
type detailScrollLogBrowser struct {
	model.Browser
	requests []contract.ScrollRequest
}

// Scroll 收集详情滚轮参数，不访问真实招聘页面。
func (b *detailScrollLogBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.requests = append(b.requests, request)
	return contract.ScrollResult{}, nil
}

// TestLoggedRuntimePreservesPlatformDetailBrowsing 验证三个 DOM 详情平台经日志包装后仍执行原有滚轮操作。
func TestLoggedRuntimePreservesPlatformDetailBrowsing(t *testing.T) {
	for _, platformID := range []string{"hliepin", "liepin", "zhaopin"} {
		t.Run(platformID, func(t *testing.T) {
			runtime, err := platform.RuntimeFor(platformID)
			if err != nil {
				t.Fatal(err)
			}
			config, err := platform.LoadConfig(platformID)
			if err != nil {
				t.Fatal(err)
			}
			original, ok := runtime.(model.DetailBrowser)
			if !ok {
				t.Fatal("测试平台原有详情浏览能力缺失")
			}
			collector := &runtimeLogCollector{}
			wrapped := newLoggedRuntime(runtime, "task", collector)
			detailBrowser, ok := wrapped.(model.DetailBrowser)
			if !ok {
				t.Fatal("日志包装丢失了详情浏览能力")
			}
			originalBrowser := &detailScrollLogBrowser{}
			wrappedBrowser := &detailScrollLogBrowser{}
			if err := original.BrowseCandidateDetail(context.Background(), originalBrowser, config, model.Candidate{}); err != nil {
				t.Fatal(err)
			}
			if err := detailBrowser.BrowseCandidateDetail(context.Background(), wrappedBrowser, config, model.Candidate{}); err != nil {
				t.Fatal(err)
			}
			if len(wrappedBrowser.requests) != 1 || !reflect.DeepEqual(wrappedBrowser.requests, originalBrowser.requests) {
				t.Fatalf("包装后没有执行相同的详情滚轮操作：%+v", wrappedBrowser.requests)
			}
			if len(collector.events) != 1 || collector.events[0] != (runtimeLogEvent{"browse_candidate_detail", "success"}) {
				t.Fatalf("详情浏览结果日志不完整：%+v", collector.events)
			}
		})
	}
}

// TestLoggedRuntimeDoesNotInventDetailBrowsing 验证不支持详情浏览的平台不会因包装而被误判为支持。
func TestLoggedRuntimeDoesNotInventDetailBrowsing(t *testing.T) {
	for _, platformID := range []string{"fixture", "boss"} {
		t.Run(platformID, func(t *testing.T) {
			var runtime model.Runtime = runtimeLogFixture{}
			if platformID == "boss" {
				var err error
				runtime, err = platform.RuntimeFor(platformID)
				if err != nil {
					t.Fatal(err)
				}
			}
			collector := &runtimeLogCollector{}
			wrapped := newLoggedRuntime(runtime, "task", collector)
			if _, ok := wrapped.(model.DetailBrowser); ok {
				t.Fatal("日志包装凭空增加了详情浏览能力")
			}
			if len(collector.events) != 0 {
				t.Fatalf("未浏览详情却记录了完成日志：%+v", collector.events)
			}
		})
	}
}

// detailRuntimeLogFixture 模拟详情浏览的参数传递、错误与 panic。
type detailRuntimeLogFixture struct {
	runtimeLogFixture
	browse func(context.Context, model.Browser, model.Config, model.Candidate) error
}

// BrowseCandidateDetail 把原始调用参数传给测试断言。
func (f detailRuntimeLogFixture) BrowseCandidateDetail(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) error {
	return f.browse(ctx, browser, config, candidate)
}

// TestLoggedRuntimeDetailBrowsingPreservesArgumentsAndError 验证详情浏览原样传参并保留原始错误。
func TestLoggedRuntimeDetailBrowsingPreservesArgumentsAndError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	browser := &detailScrollLogBrowser{}
	config := model.Config{ID: "fixture"}
	candidate := model.Candidate{Index: 7, Fingerprint: "candidate-7"}
	failure := errors.New("滚动目标没有找到")
	calls := 0
	runtime := detailRuntimeLogFixture{browse: func(actualCtx context.Context, actualBrowser model.Browser, actualConfig model.Config, actualCandidate model.Candidate) error {
		calls++
		if actualCtx != ctx || actualBrowser != browser || !reflect.DeepEqual(actualConfig, config) || !reflect.DeepEqual(actualCandidate, candidate) {
			t.Fatal("详情浏览调用参数被日志层改变")
		}
		return failure
	}}
	collector := &runtimeLogCollector{}
	wrapped := newLoggedRuntime(runtime, "task", collector).(model.DetailBrowser)
	if err := wrapped.BrowseCandidateDetail(ctx, browser, config, candidate); err != failure || calls != 1 {
		t.Fatalf("详情浏览错误或调用次数被改变：err=%v calls=%d", err, calls)
	}
	if len(collector.events) != 1 || collector.events[0] != (runtimeLogEvent{"browse_candidate_detail", "failed"}) || collector.errors[0] != failure {
		t.Fatalf("详情浏览失败日志缺失或错误被改变：%+v", collector.events)
	}
}

// TestLoggedRuntimeDetailBrowsingPreservesPanic 验证详情浏览异常写日志并原样传播给任务边界。
func TestLoggedRuntimeDetailBrowsingPreservesPanic(t *testing.T) {
	panicValue := errors.New("详情浏览异常")
	runtime := detailRuntimeLogFixture{browse: func(context.Context, model.Browser, model.Config, model.Candidate) error {
		panic(panicValue)
	}}
	collector := &runtimeLogCollector{}
	wrapped := newLoggedRuntime(runtime, "task", collector).(model.DetailBrowser)
	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("详情浏览 panic 被吞掉或改变：%v", recovered)
		}
		if len(collector.events) != 1 || collector.events[0] != (runtimeLogEvent{"browse_candidate_detail", "panic"}) || collector.errors[0] == nil {
			t.Fatalf("详情浏览 panic 没有结果日志：%+v", collector.events)
		}
	}()
	_ = wrapped.BrowseCandidateDetail(context.Background(), nil, model.Config{}, model.Candidate{})
}
