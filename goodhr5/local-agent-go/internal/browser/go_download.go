// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"context"
	"os"
)

// GetCookies 导出 Cookie。
func (c *GoController) GetCookies(ctx context.Context) ([]Cookie, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return nil, err
	}
	result, err := page.client.Call(ctx, "Network.getAllCookies", nil)
	if err != nil {
		return nil, err
	}
	rawCookies, _ := result["cookies"].([]any)
	cookies := make([]Cookie, 0, len(rawCookies))
	for _, raw := range rawCookies {
		item, _ := raw.(map[string]any)
		cookies = append(cookies, Cookie{
			Name:     stringFromAny(item["name"]),
			Value:    stringFromAny(item["value"]),
			Domain:   stringFromAny(item["domain"]),
			Path:     stringFromAny(item["path"]),
			Expires:  floatFromAny(item["expires"]),
			HTTPOnly: goBoolFromAny(item["httpOnly"]),
			Secure:   goBoolFromAny(item["secure"]),
		})
	}
	return cookies, nil
}

// SetCookies 导入 Cookie。
func (c *GoController) SetCookies(ctx context.Context, cookies []Cookie) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	params := make([]map[string]any, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		item := map[string]any{"name": cookie.Name, "value": cookie.Value}
		if cookie.Domain != "" {
			item["domain"] = cookie.Domain
		}
		if cookie.Path != "" {
			item["path"] = cookie.Path
		}
		if cookie.Expires > 0 {
			item["expires"] = cookie.Expires
		}
		item["httpOnly"] = cookie.HTTPOnly
		item["secure"] = cookie.Secure
		params = append(params, item)
	}
	_, err = page.client.Call(ctx, "Network.setCookies", map[string]any{"cookies": params})
	return err
}

// SetDownloadDir 设置下载目录。
func (c *GoController) SetDownloadDir(ctx context.Context, dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setDownloadDirLocked(ctx, dir)
}

// ListDownloads 读取下载记录。
func (c *GoController) ListDownloads(ctx context.Context) ([]DownloadRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return append([]DownloadRecord(nil), c.downloads...), nil
}

func (c *GoController) setDownloadDirLocked(ctx context.Context, dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	c.downloadsPath = dir
	_, err = page.client.Call(ctx, "Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": dir})
	return err
}

func cookiesFromAny(value any) []Cookie {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	cookies := make([]Cookie, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cookies = append(cookies, Cookie{
			Name:     stringFromAny(m["name"]),
			Value:    stringFromAny(m["value"]),
			Domain:   stringFromAny(m["domain"]),
			Path:     stringFromAny(m["path"]),
			Expires:  floatFromAny(m["expires"]),
			HTTPOnly: goBoolFromAny(m["httpOnly"]),
			Secure:   goBoolFromAny(m["secure"]),
		})
	}
	return cookies
}
