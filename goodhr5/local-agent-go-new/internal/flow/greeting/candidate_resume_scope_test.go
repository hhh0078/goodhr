// Package greeting 文件作用：验证智联正常断点的旧版摘要迁移和条件隔离；BOSS不再使用历史断点。
package greeting

import (
	"context"
	"encoding/json"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
	"path/filepath"
	"strings"
	"testing"
)

// TestResumeStopRestartWithScopeMigration 使用正式智联运行时、SQLite重开及新任务，验证默认开启/显式开启切换和旧记录均接着下一人。
func TestResumeStopRestartWithScopeMigration(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, direction := range []bool{false, true} {
			t.Run(map[bool]string{false: "新版", true: "旧版"}[legacy]+map[bool]string{false: "默认到开启", true: "开启到默认"}[direction], func(t *testing.T) {
				ctx := context.Background()
				p, browser := resumeFlowFixture()
				cfg, err := platform.LoadConfig("zhaopin")
				if err != nil {
					t.Fatal(err)
				}
				p.Platform = cfg
				browser.url = cfg.ManualSearchURL
				for i := range browser.rows {
					browser.rows[i].Fields["education"] = browser.rows[i].Fields["basic_info"]
					browser.rows[i].Fields["card_text"] = "合成普通卡片"
				}
				enabled := true
				if direction {
					p.Request.EnableGreet = &enabled
				}
				path := filepath.Join(t.TempDir(), "resume.db")
				store, err := storage.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				runtime, _ := platform.RuntimeFor("zhaopin")
				session, err := (&Flow{Store: store}).prepareCandidateResume(ctx, browser, p, resumeProviderRuntime{runtime, runtime.(model.CandidateListResumer)}, &model.CandidateListCursor{})
				if err != nil {
					t.Fatal(err)
				}
				rows, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: &model.CandidateListCursor{}})
				if err != nil || len(rows) < 3 {
					t.Fatalf("读取合成名单：%v %d", err, len(rows))
				}
				if err := session.complete(ctx, rows[1], nil); err != nil {
					t.Fatal(err)
				}
				want := session.record.Point
				if legacy {
					_, hash, _, err := candidateResumeScopeHashes(p, browser.url, false)
					if err != nil {
						t.Fatal(err)
					}
					saved := session.record
					saved.ScopeHash = hash
					saved.ScopeFields = nil
					raw, _ := json.Marshal(saved)
					if err := store.SetSetting(ctx, session.key, string(raw)); err != nil {
						t.Fatal(err)
					}
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				store, err = storage.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				if direction {
					p.Request.EnableGreet = nil
				} else {
					p.Request.EnableGreet = &enabled
				}
				p.Request.TaskID = "restarted-task"
				p.Position.ScannedCount = 999
				runtime, _ = platform.RuntimeFor("zhaopin")
				cursor := &model.CandidateListCursor{}
				restored, err := (&Flow{Store: store}).prepareCandidateResume(ctx, browser, p, resumeProviderRuntime{runtime, runtime.(model.CandidateListResumer)}, cursor)
				if err != nil || restored.record.Point != want || cursor.StartIndex != 1 {
					t.Fatalf("停止重启没续上：%v %+v", err, cursor)
				}
				next, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: cursor})
				if err != nil || len(next) != 2 || !restored.completedAnchor(next[0]) || restored.completedAnchor(next[1]) || next[1].Fingerprint != rows[2].Fingerprint {
					t.Fatalf("重启没有跳过锚点继续下一人：%v %+v", err, next)
				}
				if err := restored.complete(ctx, next[1], nil); err != nil {
					t.Fatal(err)
				}
				raw, _ := store.GetSetting(ctx, restored.key)
				if strings.Contains(raw, "不可落盘") || strings.Contains(raw, "zhipin.com") || !strings.Contains(raw, "scope_fields") {
					t.Fatal("分项摘要缺失或保存了私人原文")
				}
				_, _, oldFields, _ := candidateResumeScopeHashes(p, browser.url, true)
				p.Position.Description = "真正修改的要求"
				_, _, newFields, _ := candidateResumeScopeHashes(p, browser.url, true)
				if reason := candidateResumeChangeReason(oldFields, newFields); !strings.Contains(reason, "岗位要求") || strings.Contains(reason, "页面或筛选参数") {
					t.Fatal("变化原因不准确", reason)
				}
				fresh, _ := platform.RuntimeFor("zhaopin")
				changed, err := (&Flow{Store: store}).prepareCandidateResume(ctx, browser, p, fresh, &model.CandidateListCursor{})
				if err != nil || changed.record.Point.CandidateID != "" {
					t.Fatal("真正修改条件后仍复用了旧断点", err)
				}
			})
		}
	}
}
