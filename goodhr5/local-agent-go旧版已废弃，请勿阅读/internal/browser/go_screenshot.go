// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScreenshotPage 截取页面图片。
func (c *GoController) ScreenshotPage(ctx context.Context, options ScreenshotOptions) (ScreenshotResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.screenshotLocked(ctx, options, nil)
}

// ScreenshotElement 截取元素图片。
func (c *GoController) ScreenshotElement(ctx context.Context, options ScreenshotOptions) (ScreenshotResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	selector := ElementSelector{Selector: options.Selector, Ref: options.Ref}
	clip, err := c.elementClipLocked(ctx, selector)
	if err != nil {
		return ScreenshotResult{}, err
	}
	return c.screenshotLocked(ctx, options, clip)
}

func (c *GoController) screenshotFromPayload(ctx context.Context, payload map[string]any) (ScreenshotResult, error) {
	options := ScreenshotOptions{
		Selector: firstSelectorFromAny(payload),
		Ref:      stringFromAny(payload["ref"]),
		Dir:      stringFromAny(payload["dir"]),
		Filename: stringFromAny(payload["filename"]),
		FullPage: goBoolFromAny(payload["full_page"]),
	}
	if options.Dir == "" {
		options.Dir = stringFromAny(payload["directory"])
	}
	if options.Selector != "" || options.Ref != "" {
		if result, err := c.ScreenshotElement(ctx, options); err == nil {
			return result, nil
		}
	}
	return c.ScreenshotPage(ctx, options)
}

func (c *GoController) screenshotLocked(ctx context.Context, options ScreenshotOptions, clip map[string]any) (ScreenshotResult, error) {
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return ScreenshotResult{}, err
	}
	dir := strings.TrimSpace(options.Dir)
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "goodhr-screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ScreenshotResult{}, err
	}
	filename := safeFilename(options.Filename)
	if filename == "" {
		filename = fmt.Sprintf("go-screenshot-%d.png", time.Now().UnixMilli())
	}
	params := map[string]any{"format": "png", "fromSurface": true, "captureBeyondViewport": true}
	if clip != nil {
		params["clip"] = clip
	}
	result, err := page.client.Call(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return ScreenshotResult{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(stringFromAny(result["data"]))
	if err != nil {
		return ScreenshotResult{}, err
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return ScreenshotResult{}, err
	}
	return ScreenshotResult{Path: path, File: path}, nil
}

func safeFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." {
		return fmt.Sprintf("go-screenshot-%d.png", time.Now().UnixMilli())
	}
	return name
}
