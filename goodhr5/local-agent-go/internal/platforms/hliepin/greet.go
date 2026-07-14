// Package hliepin 文件作用：承载 greet.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// GreetCandidate 执行猎聘猎头端候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	item := candidateItemElement(candidate, cfg)
	greetBtn := platformElement(cfg, "actions", "greetBtn")
	if greetBtn == nil {
		greetBtn = map[string]any{"selector": "button"}
	} else {
		greetBtn["selectors"] = []any{"button"}
	}
	_, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{"index": intFromMap(candidate, "card_index"), "item": item, "clickTarget": greetBtn, "timeout": 10000})
	return err
}
