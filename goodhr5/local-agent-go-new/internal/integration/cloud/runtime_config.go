// Package cloud 文件作用：使用本次网页登录凭证读取既有运行组件清单，不保存凭证或另建配置来源。
package cloud

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"goodhr5/local-agent-go-new/internal/runtime"
)

// RuntimeManifest 只读取既有配置接口中的运行组件，平台选择和安装校验由运行组件管理器完成。
func (c *Client) RuntimeManifest(ctx context.Context, token string) (runtime.Manifest, error) {
	var response struct {
		Config struct {
			RuntimeComponents runtime.Manifest `json:"runtime_components"`
		} `json:"config"`
	}
	if strings.TrimSpace(token) == "" {
		return runtime.Manifest{}, fmt.Errorf("登录后才能检查浏览器更新，请重新打开后台")
	}
	if err := c.do(ctx, http.MethodGet, "/api/runtime/config", token, nil, &response); err != nil {
		return runtime.Manifest{}, err
	}
	if len(response.Config.RuntimeComponents.Camoufox) == 0 {
		return runtime.Manifest{}, fmt.Errorf("浏览器更新清单没读到，请刷新后台重试")
	}
	return response.Config.RuntimeComponents, nil
}
