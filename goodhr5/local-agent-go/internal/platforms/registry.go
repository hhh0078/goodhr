// Package platforms 负责按平台 ID 分发本地平台运行时实现。
package platforms

import (
	"fmt"
	"strings"

	"goodhr5/local-agent-go/internal/platformcore"
	"goodhr5/local-agent-go/internal/platforms/boss"
	"goodhr5/local-agent-go/internal/platforms/hliepin"
)

var registry = map[string]platformcore.Runtime{
	"boss":    boss.NewRuntime(),
	"hliepin": hliepin.NewRuntime(),
}

// RuntimeFor 按平台 ID 返回平台运行时。
// platformID 为平台标识，例如 boss。
func RuntimeFor(platformID string) (platformcore.Runtime, error) {
	id := strings.ToLower(strings.TrimSpace(platformID))
	if id == "" {
		id = "boss"
	}
	runtime, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("平台 %s 暂未实现本地运行时", platformID)
	}
	return runtime, nil
}
