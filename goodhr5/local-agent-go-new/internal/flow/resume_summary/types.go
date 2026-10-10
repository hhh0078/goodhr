// Package resume_summary 文件作用：定义本地只生成摘要草稿的合同；凭证、模型密钥和配对密钥不进入持久化或响应。
package resume_summary

import (
	"context"
	"errors"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

var ErrNotFound = errors.New("摘要草稿没找到，或属于其它账号和插件安装")
var ErrConflict = errors.New("这次请求编号已用于其它简历，请重新发起")
var ErrBusy = errors.New("摘要还在处理中，请稍等或先取消")

// CloudConfig 复用用户有效模型配置，流程不具备候选人保存、备注或岗位写入能力。
type CloudConfig interface {
	EffectiveAI(context.Context, string) (cloud.AIConfig, error)
}

// Summarizer 用于摘要AI调用，可由无付费测试替身替换。
type Summarizer interface {
	SummarizeResume(context.Context, cloud.AIConfig, cloud.LibraryCandidate) (string, error)
}

// AuthCheck 每次开始AI和返回结果前重新校验完整网页登录与插件配对代次。
type AuthCheck func(context.Context) error

// Task 只对授权调用者返回摘要草稿和处理状态。
type Task struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Summary         string `json:"ai_summary"`
	Model           string `json:"model"`
	Error           string `json:"error"`
	PossiblyCharged bool   `json:"possibly_charged"`
}

// storedTask 保存在现有SQLite设置表中，不包含任何会话或模型凭证。
type storedTask struct {
	Task        Task                   `json:"task"`
	Owner       string                 `json:"owner"`
	Scope       string                 `json:"scope"`
	Fingerprint string                 `json:"fingerprint"`
	Candidate   cloud.LibraryCandidate `json:"candidate"`
	Started     bool                   `json:"started"`
}

// SettingsStore 沿用简历导入使用的本地SQLite键值合同。
type SettingsStore interface {
	GetSetting(context.Context, string) (string, error)
	SetSetting(context.Context, string, string) error
}

// SourceCandidate 接收职业事实字段，忽略头像、来源授权、旧摘要和不同旧版本的年龄表示。
type SourceCandidate struct {
	cloud.StructuredCandidate
	ID                 string   `json:"id"`
	CandidateID        string   `json:"candidate_id"`
	IsBuiltin          bool     `json:"is_builtin"`
	Readonly           bool     `json:"readonly"`
	SourceType         string   `json:"source_type"`
	BasicInfo          string   `json:"basic_info"`
	CurrentRegion      string   `json:"current_region"`
	ExpectedRegion     string   `json:"expected_region"`
	CurrentPosition    string   `json:"current_position"`
	ExpectedSalaryText string   `json:"expected_salary_text"`
	CurrentSalaryText  string   `json:"current_salary_text"`
	Tags               []string `json:"tags"`
}

// Candidate 把边界已经校验的源字段转换为本地统一职业资料。
func (s SourceCandidate) Candidate() cloud.LibraryCandidate {
	id := s.ID
	if id == "" || s.CandidateID == cloud.BuiltinCandidateID {
		id = s.CandidateID
	}
	return cloud.LibraryCandidate{StructuredCandidate: s.StructuredCandidate, ID: id, IsBuiltin: s.IsBuiltin, Readonly: s.Readonly, SourceType: s.SourceType, BasicInfo: s.BasicInfo, CurrentRegion: s.CurrentRegion, ExpectedRegion: s.ExpectedRegion, CurrentPosition: s.CurrentPosition, ExpectedSalaryText: s.ExpectedSalaryText, CurrentSalaryText: s.CurrentSalaryText, Tags: s.Tags}
}
