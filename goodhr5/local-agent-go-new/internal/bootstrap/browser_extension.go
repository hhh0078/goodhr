// 文件作用：在所有 Firefox 冷启动入口统一准备最新版临时简历插件，保留原 Profile 和其它扩展。
package bootstrap

import (
	"context"
	"fmt"
	"log"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/profile"
	"goodhr5/local-agent-go-new/internal/runtime/browserextension"
)

// firefoxBeforeStart 组装只在冷启动执行的下载步骤；来源目录替换与浏览器登录资料完全分离。
func firefoxBeforeStart(cfg config.Config) client.BeforeBrowserStart {
	return func(ctx context.Context, request contract.BrowserStartRequest) (contract.BrowserStartRequest, error) {
		if err := profile.AssertFirefoxProfilesIdle(cfg.ProfilesDir); err != nil {
			return request, fmt.Errorf("浏览器窗口还留着，请先关闭后再打开：%w", err)
		}
		destination, err := browserextension.ManagedFirefoxDestination(cfg.ExtensionsDir)
		if err != nil {
			return request, err
		}
		result, err := browserextension.PrepareFirefoxTemporary(ctx, browserextension.Options{CloudBaseURL: cfg.CloudURL, DestinationDir: destination})
		if err != nil {
			return request, fmt.Errorf("简历插件暂时没准备好，请检查网络后重新打开浏览器：%w", err)
		}
		request.ExtensionPaths = browserextension.WithManagedFirefoxPath(request.ExtensionPaths, result.Directory)
		log.Printf("event=firefox_extension_ready version=%q cached=%t already_current=%t", result.Version, result.UsedCached, result.AlreadyCurrent)
		return request, nil
	}
}
