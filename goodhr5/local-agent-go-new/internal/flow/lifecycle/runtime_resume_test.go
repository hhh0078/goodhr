// Package lifecycle 文件作用：覆盖正式平台注册与日志封装的断点交付，防止平台测试通过而正式任务从不保存。
package lifecycle

import (
	"context"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestLoggedRuntimePreservesCandidateResume 验证全部正式平台及不支持的旧实现经过日志封装后，准确交付原有断点对象并启用追踪。
func TestLoggedRuntimePreservesCandidateResume(t *testing.T) {
	for _, id := range []string{"boss", "zhaopin", "51job", "liepin", "hliepin"} {
		t.Run(id, func(t *testing.T) {
			runtime, err := platform.RuntimeFor(id)
			if err != nil {
				t.Fatal(err)
			}
			original, supported := runtime.(model.CandidateListResumer)
			wrapped := newLoggedRuntime(runtime, "resume-assembly", nil)
			provider, ok := wrapped.(model.CandidateListResumeProvider)
			if !ok || (provider.CandidateListResumer() != nil) != supported || provider.CandidateListResumer() != original {
				t.Fatal("正式日志封装丢失或伪造断点能力")
			}
			if supported {
				cursor := &model.CandidateListCursor{StartIndex: 9}
				found, err := provider.CandidateListResumer().RestoreCandidateList(context.Background(), nil, model.Config{}, model.CandidateResumePoint{}, cursor)
				if err != nil || found || cursor.StartIndex != 0 {
					t.Fatalf("断点追踪没有初始化：%v %v %+v", found, err, cursor)
				}
			}
		})
	}
	wrapped := newLoggedRuntime(runtimeLogFixture{}, "legacy", nil)
	if wrapped.(model.CandidateListResumeProvider).CandidateListResumer() != nil {
		t.Fatal("旧实现被误判支持断点")
	}
}
