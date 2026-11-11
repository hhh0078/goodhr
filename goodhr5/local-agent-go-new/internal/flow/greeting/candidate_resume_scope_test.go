// Package greeting 文件作用：验证BOSS停止重启的条件等价、旧断点迁移、真实条件隔离与变化原因，所有进度仅写测试库。
package greeting

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestBossResumeEquivalentScope 验证BOSS推荐页只忽略声明的追踪参数及默认开启值，真实筛选、页面、岗位要求和禁发开关仍隔离。
func TestBossResumeEquivalentScope(t *testing.T) {
	p, _ := resumeFlowFixture()
	cfg, err := platform.LoadConfig("boss")
	if err != nil {
		t.Fatal(err)
	}
	p.Platform = cfg
	_, want, _, err := candidateResumeScopeHashes(p, cfg.EntryURL, true)
	if err != nil {
		t.Fatal(err)
	}
	// 先证明旧版确实把同一开启语义算成不同条件，覆盖用户日志中的误判路径。
	_, oldDefault, _, _ := candidateResumeScopeHashes(p, cfg.EntryURL, false)
	enabled := true
	explicit := p
	explicit.Request.EnableGreet = &enabled
	_, oldExplicit, _, _ := candidateResumeScopeHashes(explicit, cfg.EntryURL, false)
	if oldDefault == oldExplicit {
		t.Fatal("旧版误判场景未复现")
	}

	for _, kind := range []string{"默认与开启", "追踪参数", "筛选参数", "其他页面", "岗位要求", "关闭打招呼", "搜索页追踪参数"} {
		t.Run(kind, func(t *testing.T) {
			changed := p
			page := cfg.EntryURL
			equivalent := false
			switch kind {
			case "默认与开启":
				enabled := true
				changed.Request.EnableGreet = &enabled
				equivalent = true
			case "追踪参数":
				page += "?ka=fixture&lid=trace&_=" + "123"
				equivalent = true
			case "筛选参数":
				page += "?city=101&page=2"
			case "其他页面":
				page = "https://www.zhipin.com/web/chat/search"
			case "岗位要求":
				changed.Position.Description = "不同的岗位要求"
			case "关闭打招呼":
				disabled := false
				changed.Request.EnableGreet = &disabled
			case "搜索页追踪参数":
				changed.Platform.EntryURL = "https://example.invalid/recommend"
				page += "?ka=fixture"
			}
			_, got, _, err := candidateResumeScopeHashes(changed, page, true)
			if err != nil || (got == want) != equivalent {
				t.Fatalf("条件隔离不正确：%s %v", kind, err)
			}
		})
	}
	// 非BOSS平台没有配置白名单时，查询参数仍原样隔离，避免改变搜索页行为。
	p.Platform.CandidateResumeIgnoredQueryKeys = nil
	if candidateResumePageURL(p, cfg.EntryURL+"?ka=keep") != cfg.EntryURL+"?ka=keep" {
		t.Fatal("未配置平台的参数被移除")
	}
}

// TestBossStopRestartWithScopeMigration 使用正式BOSS运行时、SQLite重开及新任务，验证默认开启/显式开启切换和旧记录均接着下一人。
func TestBossStopRestartWithScopeMigration(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, direction := range []bool{false, true} {
			t.Run(map[bool]string{false: "新版", true: "旧版"}[legacy]+map[bool]string{false: "默认到开启", true: "开启到默认"}[direction], func(t *testing.T) {
				ctx := context.Background()
				p, browser := resumeFlowFixture()
				cfg, err := platform.LoadConfig("boss")
				if err != nil {
					t.Fatal(err)
				}
				p.Platform = cfg
				browser.url = cfg.EntryURL
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
				runtime, _ := platform.RuntimeFor("boss")
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
				if !legacy {
					browser.url += "?ka=new&lid=another&_=456"
				}
				runtime, _ = platform.RuntimeFor("boss")
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
				fresh, _ := platform.RuntimeFor("boss")
				changed, err := (&Flow{Store: store}).prepareCandidateResume(ctx, browser, p, fresh, &model.CandidateListCursor{})
				if err != nil || changed.record.Point.CandidateID != "" {
					t.Fatal("真正修改条件后仍复用了旧断点", err)
				}
			})
		}
	}
}
