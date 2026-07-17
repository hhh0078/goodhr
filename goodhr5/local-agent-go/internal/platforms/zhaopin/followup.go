// Package zhaopin 文件作用：提供智联招聘基础筛选和打招呼后索要信息的平台扩展入口。
package zhaopin

import (
	"context"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// ApplyBasicFilters 保留智联招聘基础筛选入口，当前不执行页面操作。
func (r *Runtime) ApplyBasicFilters(context.Context, platformcore.Executor, cloudapi.PlatformConfig, map[string]any) error {
	return nil
}

// RequestCandidateInfo 保留智联招聘索要候选人信息入口，当前直接返回。
func (r *Runtime) RequestCandidateInfo(context.Context, platformcore.Executor, cloudapi.PlatformConfig, platformcore.Candidate, platformcore.CandidateInfoRequest) error {
	return nil
}
