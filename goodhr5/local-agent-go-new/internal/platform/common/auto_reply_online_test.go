// Package common 本文件验证在线资料的固定读取模式、临时截图清理和可信字段解析，不访问真实招聘页面。
package common

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// onlineProfileTestBrowser 模拟当前详情层、DOM正文与已有真实滚轮截图接口。
type onlineProfileTestBrowser struct {
	model.Browser
	open, incomplete                     bool
	name, text                           string
	imageDirectory                       string
	bodyReads, clicks, keys, screenshots int
}

// Read 只提供测试明确配置的详情字段，便于证明BOSS不会走DOM正文。
func (b *onlineProfileTestBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	value := ""
	switch request.Selector.Description {
	case "name":
		value = b.name
	case "body":
		b.bodyReads++
		value = b.text
	case "frame-url":
		value = "https://example.test/frame/resume?source=chat"
	}
	return contract.ReadResult{Value: value}, nil
}

// ListPages 始终返回同一原页，不模拟额外标签。
func (b *onlineProfileTestBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: "https://example.test/chat", Current: true}}}, nil
}

// FindAll 返回当前详情层是否仍可见。
func (b *onlineProfileTestBrowser) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if b.open {
		return []contract.FindAllItem{{Index: 0, TotalMatches: 1}}, nil
	}
	return nil, nil
}

// Click 模拟从已核实会话打开其在线详情。
func (b *onlineProfileTestBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks++
	b.open = true
	return contract.ClickResult{Clicked: true}, nil
}

// PressKey 只接受原页上目标详情层的Esc关闭。
func (b *onlineProfileTestBrowser) PressKey(_ context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if request.Key != "Escape" || request.ExpectedPageURL != "https://example.test/chat" || request.Target == nil {
		return contract.KeyboardPressResult{}, errors.New("详情关闭范围不完整")
	}
	b.keys++
	b.open = false
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// LongScreenshot 在本次独立临时目录生成真实PNG，模拟Worker已有接口而非注入页面脚本。
func (b *onlineProfileTestBrowser) LongScreenshot(_ context.Context, request contract.LongScreenshotRequest) (contract.LongScreenshotResult, error) {
	b.screenshots++
	b.imageDirectory = request.Directory
	path := filepath.Join(request.Directory, request.Filename)
	file, err := os.Create(path)
	if err != nil {
		return contract.LongScreenshotResult{}, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 32, 32))
	canvas.Set(2, 2, color.RGBA{R: 100, G: 150, B: 50, A: 255})
	if err := png.Encode(file, canvas); err != nil {
		_ = file.Close()
		return contract.LongScreenshotResult{}, err
	}
	if err := file.Close(); err != nil {
		return contract.LongScreenshotResult{}, err
	}
	return contract.LongScreenshotResult{Count: 1, Complete: !b.incomplete, Parts: []contract.ScreenshotPart{{Path: path, Filename: request.Filename}}}, nil
}

// onlineProfileFixture 创建只包含逻辑键的在线资料配置，不依赖任何真实平台选择器。
func onlineProfileFixture(platform string) (*onlineProfileTestBrowser, model.Config, model.AutoReplyConversationSnapshot) {
	cfg := model.Config{ID: platform, MessagesURL: "https://example.test/chat", Selectors: map[string]contract.SelectorSpec{}}
	for key, description := range map[string]string{
		"message.online_profile_entry": "entry", "message.online_profile_panel": "panel", "message.online_profile_body": "body", "message.online_profile_name": "name", "message.online_profile_close": "close", "message.online_profile_screenshot": "image",
		"message.online_profile_frame_url": "frame-url",
	} {
		cfg.Selectors[key] = contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}, Description: description}
	}
	b := &onlineProfileTestBrowser{name: "目标候选人", text: "目标候选人\n男 | 28岁 | 大专\n工作经历\n2012.03 - 2019.06\n教育经历\n2008.09 - 2011.06"}
	return b, cfg, model.AutoReplyConversationSnapshot{CandidateName: b.name, PlatformCandidateID: "123456"}
}

// TestOnlineProfileModesAndTemporaryImages 验证Boss只截图交服务器AI，其余平台仅DOM，关闭恢复且临时图不残留。
func TestOnlineProfileModesAndTemporaryImages(t *testing.T) {
	for _, platform := range []string{"boss", "liepin", "zhaopin", "hliepin"} {
		b, cfg, snapshot := onlineProfileFixture(platform)
		restored := 0
		verify := func(context.Context) error { return nil }
		restore := func(context.Context) error { restored++; return nil }
		profile, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, strings.TrimSpace)
		if err != nil || b.open || b.keys != 1 || restored != 1 {
			t.Fatalf("%s没有恢复原聊天：%+v %v", platform, profile, err)
		}
		if platform == "boss" {
			if profile.Mode != "ai" || profile.RawText != "" || len(profile.Screenshots) != 1 || b.bodyReads != 0 || !strings.HasPrefix(profile.SourceKey, "online-image:") {
				t.Fatalf("Boss读取模式错误：%+v body=%d", profile, b.bodyReads)
			}
			if _, err := os.Stat(b.imageDirectory); !os.IsNotExist(err) {
				t.Fatal("Boss本次临时截图目录未清理")
			}
		} else if profile.Mode != "dom" || len(profile.Screenshots) != 0 || b.screenshots != 0 || profile.Age == nil || *profile.Age != 28 || profile.EducationLevel != "大专" || profile.EarliestCompanyYear == nil || *profile.EarliestCompanyYear != 2012 || profile.EarliestSchoolYear == nil || *profile.EarliestSchoolYear != 2008 {
			t.Fatalf("%s DOM字段或模式错误：%+v", platform, profile)
		}
	}
}

// TestOnlineProfileUnavailableAndPartialCapture 验证缺入口不点击，不完整长图不交AI且仍关闭当前详情并清理。
func TestOnlineProfileUnavailableAndPartialCapture(t *testing.T) {
	b, cfg, snapshot := onlineProfileFixture("boss")
	delete(cfg.Selectors, "message.online_profile_entry")
	verify := func(context.Context) error { return nil }
	if _, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, verify, nil); !errors.Is(err, model.ErrAutoReplyOnlineProfileUnavailable) || b.clicks != 0 {
		t.Fatalf("缺入口仍操作或未返回明确不可用：%v", err)
	}
	b, cfg, snapshot = onlineProfileFixture("boss")
	b.incomplete = true
	profile, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, verify, nil)
	if err == nil || len(profile.Screenshots) != 0 || b.open {
		t.Fatalf("不完整截图被当作完整资料：%+v %v", profile, err)
	}
	if _, err := os.Stat(b.imageDirectory); !os.IsNotExist(err) {
		t.Fatal("失败采集的临时图未清理")
	}
}

// TestOnlineProfileTextKeepsConflictsUnknown 验证年龄学历冲突时留空，项目背景年份不参与最早工作年份。
func TestOnlineProfileTextKeepsConflictsUnknown(t *testing.T) {
	profile := ParseAutoReplyOnlineProfileText("目标\n28岁 30岁\n大专 本科\n工作经历\n2018.01 - 2020.02\n项目经历\n1999.01 - 2000.01\n教育经历\n2014.09 - 2017.06", "目标")
	if profile.Age != nil || profile.EducationLevel != "" || profile.EarliestCompanyYear == nil || *profile.EarliestCompanyYear != 2018 || profile.EarliestSchoolYear == nil || *profile.EarliestSchoolYear != 2014 {
		t.Fatalf("冲突字段被猜测或项目年份混入工作：%+v", profile)
	}
}
