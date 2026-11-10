// Package common 本文件通过现有真实滚轮长截图取在线资料图片，临时文件只在当前采集内存活。
package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// onlineProfileScreenshotBrowser 复用Worker已有真实滚轮截图，不引入第二条浏览器链路。
type onlineProfileScreenshotBrowser interface {
	LongScreenshot(context.Context, contract.LongScreenshotRequest) (contract.LongScreenshotResult, error)
}

// captureAutoReplyOnlineProfile 截取完整详情并复用旧拼图算法，只返回有上限的PNG字节给服务器AI。
func captureAutoReplyOnlineProfile(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) ([][]byte, string, error) {
	if snapshot.PlatformCandidateID == "" {
		return nil, "", fmt.Errorf("在线资料截图缺少当前候选人原生编号")
	}
	frameURL, found, err := ReadOptional(ctx, browser, cfg, "message.online_profile_frame_url")
	if err != nil || !found || frameURL == "" {
		return nil, "", fmt.Errorf("在线资料所在可见详情框架地址尚未核实")
	}
	screenshot, ok := browser.(onlineProfileScreenshotBrowser)
	if !ok {
		return nil, "", fmt.Errorf("在线资料截图组件还没准备好")
	}
	target, err := RequiredSelector(cfg, "message.online_profile_screenshot")
	if err != nil {
		return nil, "", err
	}
	var anchor *contract.SelectorSpec
	if configured, ok := cfg.Selectors["message.online_profile_scroll"]; ok {
		anchor = &configured
	}
	directory, err := os.MkdirTemp("", "goodhr-online-profile-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(directory)
	result, err := screenshot.LongScreenshot(ctx, contract.LongScreenshotRequest{Target: target, WheelAnchor: anchor, Directory: directory, Filename: "profile.png", Distance: 520, MaxParts: 20, WaitMS: 180})
	if err != nil {
		return nil, "", err
	}
	if !result.Complete || len(result.Parts) == 0 || len(result.Parts) > 20 {
		return nil, "", fmt.Errorf("在线资料长图尚未完整，暂不交给AI猜测")
	}
	for _, part := range result.Parts {
		realPath, err := filepath.EvalSymlinks(part.Path)
		if err != nil {
			return nil, "", err
		}
		realDirectory, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return nil, "", err
		}
		relative, err := filepath.Rel(realDirectory, realPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, "", fmt.Errorf("在线资料截图不属于本次临时目录，暂不读取")
		}
		info, err := os.Stat(realPath)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 8<<20 {
			return nil, "", fmt.Errorf("在线资料分段截图为空或超出大小上限")
		}
	}
	stitched, err := StitchScreenshotParts(result.Parts, filepath.Join(directory, "profile-stitched.png"))
	if err != nil {
		return nil, "", err
	}
	if stitched.Size <= 0 || stitched.Size > 8<<20 {
		return nil, "", fmt.Errorf("在线资料长图超过本次AI读取上限")
	}
	content, err := os.ReadFile(stitched.Path)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(content)
	afterURL, found, err := ReadOptional(ctx, browser, cfg, "message.online_profile_frame_url")
	if err != nil || !found || afterURL != frameURL {
		return nil, "", fmt.Errorf("在线资料截图期间详情框架已变化，暂不使用")
	}
	return [][]byte{content}, "online-image:" + HashText(snapshot.PlatformCandidateID+"\n"+frameURL+"\n"+hex.EncodeToString(digest[:])), nil
}
