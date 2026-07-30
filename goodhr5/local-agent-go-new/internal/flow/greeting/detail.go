// Package greeting 文件作用：处理候选人详情分段截图、OCR 和多模态图片读取。
package greeting

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// readDetailWithOCR 截取当前详情页并在本地识别文字。
func (f *Flow) readDetailWithOCR(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate, detail model.CandidateDetail) (model.CandidateDetail, error) {
	parts, err := f.captureDetailScreenshots(ctx, prepared, candidate)
	if err != nil {
		return detail, err
	}
	ocrTexts := make([]string, 0, len(parts))
	for _, part := range parts {
		result, recognizeErr := f.OCR.Recognize(ctx, part.Path)
		if recognizeErr != nil {
			if ocr.IsNoText(recognizeErr) {
				continue
			}
			return detail, recognizeErr
		}
		if text := strings.TrimSpace(result.Text); text != "" {
			ocrTexts = append(ocrTexts, text)
		}
	}
	if len(ocrTexts) == 0 {
		return detail, &ocr.Error{Code: ocr.ErrorNoText, Message: "候选人详情截图没有识别到文字"}
	}
	detail.Text = strings.TrimSpace(detail.Text + "\n" + strings.Join(ocrTexts, "\n"))
	return detail, nil
}

// readDetailImages 读取候选人详情分段截图，供多模态 AI 在本次任务内使用。
func (f *Flow) readDetailImages(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate) ([][]byte, error) {
	parts, err := f.captureDetailScreenshots(ctx, prepared, candidate)
	if err != nil {
		return nil, err
	}
	images := make([][]byte, 0, len(parts))
	for _, part := range parts {
		content, readErr := os.ReadFile(part.Path)
		if readErr != nil {
			return nil, fmt.Errorf("读取候选人详情截图失败：%w", readErr)
		}
		if len(content) > 0 {
			images = append(images, content)
		}
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("候选人详情截图为空")
	}
	return images, nil
}

// captureDetailScreenshots 使用真实鼠标滚轮生成详情分段截图。
func (f *Flow) captureDetailScreenshots(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate) ([]contract.ScreenshotPart, error) {
	filename := f.nextCandidateScreenshotFilename()
	directory := f.currentTaskScreenshotsDir()
	target, ok := prepared.Platform.Selectors["candidate.detail_screenshot"]
	if !ok {
		target, ok = prepared.Platform.Selectors["candidate.detail"]
	}
	if ok {
		var anchor *contract.SelectorSpec
		if configuredAnchor, exists := prepared.Platform.Selectors["candidate.detail_scroll"]; exists {
			anchor = &configuredAnchor
		}
		result, err := f.Browser.LongScreenshot(ctx, contract.LongScreenshotRequest{
			Target: target, WheelAnchor: anchor, Directory: directory,
			Filename: filename, Distance: 520, MaxParts: 20, WaitMS: 300,
		})
		if err != nil {
			return nil, err
		}
		if len(result.Parts) == 0 {
			return nil, fmt.Errorf("候选人详情长截图没有生成分段")
		}
		if prepared.Platform.Behavior.StitchDetailScreenshots {
			outputPath := filepath.Join(
				directory,
				strings.TrimSuffix(filename, filepath.Ext(filename))+".stitched.png",
			)
			part, stitchErr := stitchScreenshotParts(result.Parts, outputPath)
			if stitchErr != nil {
				return nil, fmt.Errorf("拼接候选人详情长图失败：%w", stitchErr)
			}
			return []contract.ScreenshotPart{part}, nil
		}
		return result.Parts, nil
	}
	screenshot, err := f.Browser.Screenshot(ctx, contract.ScreenshotRequest{
		Directory: directory, Filename: filename,
	})
	if err != nil {
		return nil, err
	}
	return []contract.ScreenshotPart{{
		Path: screenshot.Path, Filename: screenshot.Filename, Size: screenshot.Size,
		Index: 0, ScrollPosition: 0,
	}}, nil
}

// prepareScreenshotWorkspace 清空上一任务截图并创建只保留当前任务的固定目录。
func (f *Flow) prepareScreenshotWorkspace() error {
	root, err := filepath.Abs(f.ScreenshotsDir)
	if err != nil {
		return fmt.Errorf("读取截图目录失败：%w", err)
	}
	current := filepath.Join(root, "current-task")
	relative, err := filepath.Rel(root, current)
	if err != nil || relative != "current-task" {
		return fmt.Errorf("当前任务截图目录不安全，已经停止清理")
	}
	if err = os.RemoveAll(current); err != nil {
		return fmt.Errorf("清理上一任务截图失败：%w", err)
	}
	if err = os.MkdirAll(current, 0o755); err != nil {
		return fmt.Errorf("创建当前任务截图目录失败：%w", err)
	}
	f.screenshotMu.Lock()
	f.screenshotSeq = 0
	f.screenshotMu.Unlock()
	return nil
}

// currentTaskScreenshotsDir 返回只保留当前任务图片的固定目录。
func (f *Flow) currentTaskScreenshotsDir() string {
	return filepath.Join(f.ScreenshotsDir, "current-task")
}

// nextCandidateScreenshotFilename 返回当前任务内按处理顺序递增的候选人截图文件名。
func (f *Flow) nextCandidateScreenshotFilename() string {
	f.screenshotMu.Lock()
	defer f.screenshotMu.Unlock()
	f.screenshotSeq++
	return fmt.Sprintf("candidate-%03d.png", f.screenshotSeq)
}
