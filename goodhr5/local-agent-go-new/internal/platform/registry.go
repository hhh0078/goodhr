// Package platform 负责按平台编号返回隔离的平台运行时。
package platform

import (
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/boss"
	"goodhr5/local-agent-go-new/internal/platform/hliepin"
	"goodhr5/local-agent-go-new/internal/platform/liepin"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/platform/zhaopin"
)

// RuntimeFor 返回指定平台运行时。
func RuntimeFor(platformID string) (model.Runtime, error) {
	switch strings.ToLower(strings.TrimSpace(platformID)) {
	case "boss":
		return boss.NewRuntime(), nil
	case "zhaopin":
		return zhaopin.NewRuntime(), nil
	case "liepin":
		return liepin.NewRuntime(), nil
	case "hliepin":
		return hliepin.NewRuntime(), nil
	default:
		return nil, fmt.Errorf("平台 %s 暂未实现", platformID)
	}
}
