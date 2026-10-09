// Package resume_import 文件作用：定义本地批量解析、草稿恢复与确认入库的合同，不保存登录凭证。
package resume_import

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

const (
	MaxFiles      = 20
	MaxFileBytes  = 10 << 20
	MaxTotalBytes = 60 << 20
)

var (
	ErrNotFound = errors.New("这批草稿没找到，或属于另一个账号")
	ErrBusy     = errors.New("这条简历正在处理，请稍等一下")
	ErrInvalid  = errors.New("这次导入内容不完整，请检查后再试")
)

// SettingsStore 复用本地 SQLite 键值表，不增加云端原件存储。
type SettingsStore interface {
	GetSetting(context.Context, string) (string, error)
	SetSetting(context.Context, string, string) error
}

// CloudAPI 只依赖已有登录、AI配置和独立简历业务接口，方便无扣费测试。
type CloudAPI interface {
	EffectiveAI(context.Context, string) (cloud.AIConfig, error)
	SaveLibraryCandidate(context.Context, string, string, json.RawMessage, string) (cloud.CandidateSaveResult, error)
	LibraryCandidateDetail(context.Context, string, string) (cloud.CandidateSaveResult, error)
	AddLibraryNote(context.Context, string, string, string, string) error
	AddLibraryRecommendation(context.Context, string, string, string, string) error
}

// ResumeParser 只生成结构化草稿，不进行岗位评分。
type ResumeParser interface {
	ParseResume(context.Context, cloud.AIConfig, string) (cloud.LibraryCandidate, error)
}

// TextExtractor 在本地从文件提取文字，扫描文件调用本地 OCR。
type TextExtractor interface {
	Extract(context.Context, string) (string, error)
}

// Upload 表示一个已在 HTTP 边界限制大小的用户文件流。
type Upload struct {
	Name   string
	Reader io.Reader
}

// Item 表示一个可独立校验、重试和保存的解析草稿。
type Item struct {
	ID          string                 `json:"id"`
	FileName    string                 `json:"file_name"`
	Status      string                 `json:"status"`
	Candidate   cloud.LibraryCandidate `json:"candidate"`
	Error       string                 `json:"error"`
	CandidateID string                 `json:"candidate_id,omitempty"`
}

// Task 表示前端可读取的批次状态，原文件路径、账号和Token不进入响应。
type Task struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Items     []Item `json:"items"`
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
}

// ConfirmItem 表示用户明确审核后的字段和独立备注、岗位选择。
type ConfirmItem struct {
	ID          string          `json:"id"`
	Candidate   json.RawMessage `json:"candidate"`
	CandidateID string          `json:"candidate_id,omitempty"`
	PositionIDs []string        `json:"position_ids,omitempty"`
	Note        string          `json:"note,omitempty"`
}

// SavedItem 表示前端独立入库后要求标记的已验证候选人编号。
type SavedItem struct {
	ID          string `json:"id"`
	CandidateID string `json:"candidate_id"`
}

// confirmation 保存云端各步骤的收据，响应丢失和附属失败都沿用同一个幂等请求。
type confirmation struct {
	Fields         json.RawMessage `json:"fields"`
	CandidateID    string          `json:"candidate_id"`
	Key            string          `json:"key"`
	Fingerprint    string          `json:"fingerprint"`
	Note           string          `json:"note"`
	PositionIDs    []string        `json:"position_ids"`
	CandidateSaved bool            `json:"candidate_saved"`
	NoteSaved      bool            `json:"note_saved"`
	PositionsSaved []string        `json:"positions_saved"`
}

// storedTask 包装本地持久字段，登录Token和运行上下文永远不序列化。
type storedTask struct {
	Task          Task                     `json:"task"`
	OwnerID       string                   `json:"owner_id"`
	CreatedAt     int64                    `json:"created_at"`
	Paths         map[string]string        `json:"paths"`
	Confirmations map[string]*confirmation `json:"confirmations"`
	AIStarted     map[string]bool          `json:"ai_started"`
}
