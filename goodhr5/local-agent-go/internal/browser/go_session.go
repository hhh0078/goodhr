// Package browser 文件作用：按职责承载实验性 Go 浏览器控制库的拆分实现。
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// StartBrowser 启动浏览器。
func (c *GoController) StartBrowser(ctx context.Context, options BrowserStartOptions) (BrowserStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunningLocked() {
		if options.UserDataDir == "" || options.UserDataDir == c.userDataDir {
			return c.statusLocked(), nil
		}
		_, _ = c.stopLocked()
	}

	if strings.TrimSpace(options.ExecutablePath) == "" {
		options.ExecutablePath = c.executablePath
	}
	executable := resolveBrowserExecutable(options.ExecutablePath)
	if executable == "" {
		return BrowserStatus{}, fmt.Errorf("Go 浏览器模式找不到 CloakBrowser，请传 executable_path 或设置 GOODHR_CLOAKBROWSER_PATH")
	}
	port, err := freeTCPPort()
	if err != nil {
		return BrowserStatus{}, err
	}
	if options.UserDataDir != "" {
		_ = os.MkdirAll(options.UserDataDir, 0o755)
	}
	if options.DownloadsPath != "" {
		_ = os.MkdirAll(options.DownloadsPath, 0o755)
	}

	args := []string{
		"--remote-debugging-address=127.0.0.1",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
	}
	if options.UserDataDir != "" {
		args = append(args, "--user-data-dir="+options.UserDataDir)
	}
	if options.Headless {
		args = append(args, "--headless=new")
	}
	if options.ViewportWidth > 0 && options.ViewportHeight > 0 {
		args = append(args, fmt.Sprintf("--window-size=%d,%d", options.ViewportWidth, options.ViewportHeight))
	}
	args = append(args, "about:blank")

	cmd := exec.CommandContext(context.Background(), executable, args...)
	if err := cmd.Start(); err != nil {
		return BrowserStatus{}, fmt.Errorf("Go 浏览器模式启动 CloakBrowser 失败：%w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitDevTools(ctx, baseURL, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return BrowserStatus{}, err
	}
	page, err := createOrFirstPage(ctx, baseURL)
	if err != nil {
		_ = cmd.Process.Kill()
		return BrowserStatus{}, err
	}
	client, err := dialCDP(ctx, page.WebSocketDebuggerURL)
	if err != nil {
		_ = cmd.Process.Kill()
		return BrowserStatus{}, err
	}
	page.client = client
	_, _ = client.Call(ctx, "Page.enable", nil)
	_, _ = client.Call(ctx, "Runtime.enable", nil)
	_, _ = client.Call(ctx, "DOM.enable", nil)

	c.cmd = cmd
	c.port = port
	c.baseURL = baseURL
	c.page = page
	c.refs = make(map[string]ElementRef)
	c.userDataDir = options.UserDataDir
	c.downloadsPath = options.DownloadsPath
	if options.DownloadsPath != "" {
		_ = c.setDownloadDirLocked(ctx, options.DownloadsPath)
	}
	return c.statusLocked(), nil
}

// StopBrowser 关闭浏览器。
func (c *GoController) StopBrowser(ctx context.Context) (BrowserStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return BrowserStatus{}, ctx.Err()
	default:
	}
	return c.stopLocked()
}

// BrowserHealth 返回浏览器运行状态。
func (c *GoController) BrowserHealth(ctx context.Context) (BrowserStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return BrowserStatus{}, ctx.Err()
	default:
	}
	if c.cmd != nil && c.cmd.ProcessState != nil && c.cmd.ProcessState.Exited() {
		_, _ = c.stopLocked()
	}
	return c.statusLocked(), nil
}

// ListPages 列出当前页面。
func (c *GoController) ListPages(ctx context.Context) ([]PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targets, err := listPages(ctx, c.baseURL)
	if err != nil {
		return nil, err
	}
	pages := make([]PageInfo, 0, len(targets))
	for index, target := range targets {
		pages = append(pages, PageInfo{Index: index, ID: target.ID, URL: target.URL, Title: target.Title})
	}
	return pages, nil
}

// UsePage 切换当前页面。
func (c *GoController) UsePage(ctx context.Context, index int) (PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	targets, err := listPages(ctx, c.baseURL)
	if err != nil {
		return PageInfo{}, err
	}
	if len(targets) == 0 {
		return PageInfo{}, fmt.Errorf("Go 浏览器模式没有可用页面")
	}
	if index < 0 || index >= len(targets) {
		index = 0
	}
	target := targets[index]
	if c.page != nil && c.page.client != nil {
		_ = c.page.client.Close()
	}
	client, err := dialCDP(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		return PageInfo{}, err
	}
	target.client = client
	c.page = target
	c.refs = make(map[string]ElementRef)
	return PageInfo{Index: index, ID: target.ID, URL: target.URL, Title: target.Title}, nil
}

// CurrentURL 读取当前页面地址。
func (c *GoController) CurrentURL(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return "", err
	}
	value, err := c.evalLocked(ctx, "location.href")
	if err != nil {
		return page.URL, nil
	}
	if text, ok := value.(string); ok {
		page.URL = text
	}
	return page.URL, nil
}

// OpenPage 打开指定页面地址。
func (c *GoController) OpenPage(ctx context.Context, rawURL string) (PageInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return PageInfo{}, err
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return PageInfo{}, fmt.Errorf("页面地址不能为空")
	}
	if _, err := page.client.Call(ctx, "Page.navigate", map[string]any{"url": rawURL}); err != nil {
		return PageInfo{}, err
	}
	_ = c.waitReadyLocked(ctx, 30*time.Second)
	page.URL = rawURL
	c.refs = make(map[string]ElementRef)
	return PageInfo{Index: 0, ID: page.ID, URL: rawURL, Title: page.Title}, nil
}

// ReloadPage 刷新当前页面。
func (c *GoController) ReloadPage(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	page, err := c.ensurePageLocked(ctx)
	if err != nil {
		return err
	}
	if _, err := page.client.Call(ctx, "Page.reload", map[string]any{"ignoreCache": false}); err != nil {
		return err
	}
	return c.waitReadyLocked(ctx, 30*time.Second)
}

// WaitPageLoad 等待页面加载完成。
func (c *GoController) WaitPageLoad(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waitReadyLocked(ctx, 30*time.Second)
}

func (c *GoController) statusLocked() BrowserStatus {
	url := ""
	if c.page != nil {
		url = c.page.URL
	}
	return BrowserStatus{
		Running:       c.isRunningLocked(),
		Worker:        WorkerModeGo,
		Experimental:  true,
		UserDataDir:   c.userDataDir,
		DownloadsPath: c.downloadsPath,
		CurrentURL:    url,
	}
}

func (c *GoController) stopLocked() (BrowserStatus, error) {
	if c.page != nil && c.page.client != nil {
		_ = c.page.client.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	c.cmd = nil
	c.port = 0
	c.baseURL = ""
	c.page = nil
	c.refs = make(map[string]ElementRef)
	c.userDataDir = ""
	c.downloadsPath = ""
	return BrowserStatus{Running: false, Worker: WorkerModeGo, Experimental: true}, nil
}

func (c *GoController) isRunningLocked() bool {
	return c.cmd != nil && c.cmd.Process != nil && (c.cmd.ProcessState == nil || !c.cmd.ProcessState.Exited()) && c.page != nil && c.page.client != nil
}

func (c *GoController) ensurePageLocked(ctx context.Context) (*goPage, error) {
	if !c.isRunningLocked() {
		return nil, fmt.Errorf("Go 浏览器尚未启动")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return c.page, nil
}

func (c *GoController) waitReadyLocked(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value, err := c.evalLocked(ctx, `document.readyState`)
		if err == nil {
			state := stringFromAny(value)
			if state == "complete" || state == "interactive" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

func waitDevTools(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/json/version", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("等待 CloakBrowser 调试端口超时")
}

func createOrFirstPage(ctx context.Context, baseURL string) (*goPage, error) {
	pages, _ := listPages(ctx, baseURL)
	if len(pages) > 0 {
		return pages[0], nil
	}
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		req, _ := http.NewRequestWithContext(ctx, method, baseURL+"/json/new?about:blank", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		var page goPage
		err = json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if err == nil && page.WebSocketDebuggerURL != "" {
			return &page, nil
		}
	}
	return nil, fmt.Errorf("Go 浏览器模式创建页面失败")
}

func listPages(ctx context.Context, baseURL string) ([]*goPage, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("Go 浏览器尚未启动")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/json/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var pages []*goPage
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return nil, err
	}
	filtered := pages[:0]
	for _, page := range pages {
		if page.WebSocketDebuggerURL != "" {
			filtered = append(filtered, page)
		}
	}
	return filtered, nil
}

func freeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func resolveBrowserExecutable(raw string) string {
	candidates := []string{strings.TrimSpace(raw), strings.TrimSpace(os.Getenv("GOODHR_CLOAKBROWSER_PATH"))}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = appendBrowserExecutableCandidates(candidates, filepath.Join(base, "runtime", "cloakbrowser"))
		candidates = appendBrowserExecutableCandidates(candidates, filepath.Join(base, "cloakbrowser"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = appendBrowserExecutableCandidates(candidates, filepath.Join(wd, "runtime", "cloakbrowser"))
		candidates = appendBrowserExecutableCandidates(candidates, filepath.Join(wd, "dist", "runtime", "cloakbrowser"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = appendBrowserExecutableCandidates(candidates, filepath.Join(home, "Library", "Application Support", "GoodHR", "runtime", "cloakbrowser"))
		candidates = appendBrowserExecutableCandidates(candidates, filepath.Join(home, "AppData", "Roaming", "GoodHR", "runtime", "cloakbrowser"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// appendBrowserExecutableCandidates 为一个运行目录补充当前系统常见浏览器文件名。
func appendBrowserExecutableCandidates(candidates []string, root string) []string {
	for _, name := range browserExecutableNames() {
		candidates = append(candidates, filepath.Join(root, name))
	}
	return candidates
}

// browserExecutableNames 返回 CloakBrowser 发布包中可能出现的可执行文件名。
func browserExecutableNames() []string {
	switch goruntime.GOOS {
	case "windows":
		return []string{"chrome.exe", "chromium.exe", "CloakBrowser.exe"}
	case "darwin":
		return []string{
			filepath.Join("Chromium.app", "Contents", "MacOS", "Chromium"),
			filepath.Join("CloakBrowser.app", "Contents", "MacOS", "CloakBrowser"),
		}
	default:
		return []string{"chrome", "chromium", "CloakBrowser"}
	}
}

func browserExecutableName() string {
	return browserExecutableNames()[0]
}

func browserStartOptionsFromPayload(payload map[string]any) BrowserStartOptions {
	return BrowserStartOptions{
		ExecutablePath: stringFromAny(firstNonEmpty(payload["executable_path"], payload["browser_path"], payload["cloakbrowser_path"])),
		UserDataDir:    stringFromAny(payload["user_data_dir"]),
		DownloadsPath:  stringFromAny(payload["downloads_path"]),
		Headless:       goBoolFromAny(payload["headless"]),
		Persistent:     goBoolFromAny(payload["persistent"]),
		ViewportWidth:  goIntFromAny(payload["viewport_width"]),
		ViewportHeight: goIntFromAny(payload["viewport_height"]),
	}
}
