// 本文件负责将已验证的网页会话交换为独立插件凭证，不向插件返回网页原始 Token。
package cloud

import (
	"context"
	"net/http"
)

// ExtensionUser 表示插件需要展示的账号信息。
type ExtensionUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// ExtensionSession 表示云端签发的受限插件凭证。
type ExtensionSession struct {
	Token     string        `json:"token"`
	ExpiresAt string        `json:"expires_at"`
	User      ExtensionUser `json:"user"`
}

// ExchangeExtensionSession 让云端重新验证网页凭证并签发独立插件会话。
func (c *Client) ExchangeExtensionSession(ctx context.Context, token string) (ExtensionSession, error) {
	var result ExtensionSession
	err := c.do(ctx, http.MethodPost, "/api/auth/extension-session", token, struct{}{}, &result)
	return result, err
}

// RevokeExtensionSession 撤销未交付或已经退出的插件凭证，不影响网页会话。
func (c *Client) RevokeExtensionSession(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodPost, "/api/auth/extension-logout", token, struct{}{}, nil)
}
