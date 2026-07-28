// Package greeting 文件作用：处理候选人详情分段截图、OCR 和多模态图片读取。
package greeting

import (
	"context"
	"fmt"
	"os"
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
	candidateKey := candidate.Fingerprint
	if strings.TrimSpace(candidateKey) == "" {
		candidateKey = fmt.Sprintf("index-%d", candidate.Index)
	}
	filename := fmt.Sprintf("%s-%s.png", prepared.Request.TaskID, candidateKey)
	if target, ok := prepared.Platform.Selectors["candidate.detail"]; ok {
		var anchor *contract.SelectorSpec
		if configuredAnchor, exists := prepared.Platform.Selectors["candidate.detail_scroll"]; exists {
			anchor = &configuredAnchor
		}
		result, err := f.Browser.LongScreenshot(ctx, contract.LongScreenshotRequest{
			Target: target, WheelAnchor: anchor, Directory: f.ScreenshotsDir,
			Filename: filename, Distance: 520, MaxParts: 20, WaitMS: 300,
		})
		if err != nil {
			return nil, err
		}
		if len(result.Parts) == 0 {
			return nil, fmt.Errorf("候选人详情长截图没有生成分段")
		}
		return result.Parts, nil
	}
	screenshot, err := f.Browser.Screenshot(ctx, contract.ScreenshotRequest{
		Directory: f.ScreenshotsDir, Filename: filename,
	})
	if err != nil {
		return nil, err
	}
	return []contract.ScreenshotPart{{
		Path: screenshot.Path, Filename: screenshot.Filename, Size: screenshot.Size,
		Index: 0, ScrollPosition: 0,
	}}, nil
}
