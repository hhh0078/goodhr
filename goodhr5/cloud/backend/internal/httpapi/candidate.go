// 本文件定义任务主流程使用的候选人统一结构。
package httpapi

import "strings"

// Candidate 表示平台侧抽取并回传给主流程的候选人统一对象。
type Candidate struct {
	ID                  string
	PlatformID          string
	PlatformCandidateID string
	Name                string
	BasicInfo           string
	EducationLevel      string
	PersonalDescription string
	RawText             string
	FilterText          string
	DetailText          string

	// 平台运行态字段，供后续点击详情/打招呼使用，不入库。
	ElementRef string
	CardIndex  int

	// 预留字段：用于保存平台个性化扩展信息。
	Ext map[string]any
}

// DisplayName 返回候选人可读名称，优先姓名，缺失时回退占位文案。
func (c Candidate) DisplayName() string {
	name := strings.TrimSpace(c.Name)
	if name != "" {
		return name
	}
	return "未知候选人"
}
