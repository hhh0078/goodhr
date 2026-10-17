// 文件作用说明：在统一的浏览器打开入口补齐当前用户鼠标速度，避免手动打开与任务启动使用不同档位。
package api

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// pageOpenMouseSpeed 优先使用显式档位，否则读取当前登录用户偏好；无登录信息时保留现有浏览器档位。
func (s *Server) pageOpenMouseSpeed(ctx context.Context, request pageOpenRequest, authorization string) (string, error) {
	if speed := strings.TrimSpace(request.MouseSpeed); speed != "" {
		if contract.NormalizeMouseSpeed(speed) != speed {
			return "", fmt.Errorf("这个鼠标速度档位暂不支持，请重新选择")
		}
		return speed, nil
	}
	token := strings.TrimSpace(request.Token)
	if header := strings.Fields(authorization); len(header) == 2 && strings.EqualFold(header[0], "Bearer") {
		token = header[1]
	}
	if token == "" {
		s.extensionMu.Lock()
		token = s.extensionToken
		s.extensionMu.Unlock()
	}
	if token == "" || s.cloud == nil {
		return "", nil
	}
	preferences, err := s.cloud.Preferences(ctx, token)
	if err != nil {
		return "", fmt.Errorf("鼠标速度设置暂时没读到，请稍后再打开浏览器：%w", err)
	}
	return contract.NormalizeMouseSpeed(preferences.MouseSpeed), nil
}
