// Package cloud 文件作用：调用独立简历库维护接口，保存人工确认的简历、备注和推荐，不改变岗位任务统计。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// LibraryCandidate 表示批量解析草稿与人工维护使用的平铺简历字段。
type LibraryCandidate struct {
	StructuredCandidate
	IsBuiltin           bool     `json:"is_builtin,omitempty"`
	Readonly            bool     `json:"readonly,omitempty"`
	ID                  string   `json:"id,omitempty"`
	RowVersion          int64    `json:"row_version,omitempty"`
	BasicInfo           string   `json:"basic_info,omitempty"`
	Gender              string   `json:"gender,omitempty"`
	Age                 *int     `json:"age,omitempty"`
	CurrentRegion       string   `json:"current_region,omitempty"`
	ExpectedRegion      string   `json:"expected_region,omitempty"`
	CurrentPosition     string   `json:"current_position,omitempty"`
	ExpectedSalaryText  string   `json:"expected_salary_text,omitempty"`
	CurrentSalaryText   string   `json:"current_salary_text,omitempty"`
	Tags                []string `json:"tags,omitempty"`
	LibraryStatus       string   `json:"library_status,omitempty"`
	AISummary           string   `json:"ai_summary,omitempty"`
	SourceType          string   `json:"source_type,omitempty"`
	PlatformID          string   `json:"platform_id,omitempty"`
	PlatformCandidateID string   `json:"platform_candidate_id,omitempty"`
	SourceURL           string   `json:"source_url,omitempty"`
	IsResume            *bool    `json:"is_resume,omitempty"`
}

const BuiltinCandidateID = "00000000-0000-4000-8000-000000000001"

// IsReadonlyCandidate 识别云端预留示例身份和只读标记，不依据姓名或联系方式误伤真实简历。
func IsReadonlyCandidate(candidate LibraryCandidate) bool {
	return strings.EqualFold(strings.TrimSpace(candidate.ID), BuiltinCandidateID) || candidate.IsBuiltin || candidate.Readonly || candidate.SourceType == "builtin" || strings.EqualFold(strings.TrimSpace(candidate.PlatformCandidateID), BuiltinCandidateID)
}

// CandidateSaveResult 返回云端确认的候选人编号和版本，供入库重试使用。
type CandidateSaveResult struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"row_version"`
}

// SaveLibraryCandidate 保存已经通过本地合同校验的平铺字段，幂等键随请求正文传递。
func (c *Client) SaveLibraryCandidate(ctx context.Context, token, candidateID string, fields json.RawMessage, key string) (CandidateSaveResult, error) {
	fields = json.RawMessage(strings.TrimSpace(string(fields)))
	var object map[string]json.RawMessage
	if json.Unmarshal(fields, &object) != nil || object == nil {
		return CandidateSaveResult{}, fmt.Errorf("简历内容格式没认出来")
	}
	// 原始对象已在导入边界转换为强类型并校验；保留字段是否省略，避免合并时清空未提交内容。
	keyJSON, _ := json.Marshal(key)
	object["idempotency_key"] = keyJSON
	encoded, err := json.Marshal(object)
	if err != nil {
		return CandidateSaveResult{}, err
	}
	payload := json.RawMessage(encoded)
	method, path := http.MethodPost, "/api/candidates"
	if strings.TrimSpace(candidateID) != "" {
		method, path = http.MethodPatch, "/api/candidates/"+url.PathEscape(candidateID)
	}
	var response struct {
		Candidate CandidateSaveResult `json:"candidate"`
	}
	if err := c.do(ctx, method, path, token, payload, &response); err != nil {
		return CandidateSaveResult{}, err
	}
	if response.Candidate.ID == "" {
		return CandidateSaveResult{}, fmt.Errorf("云端没有确认简历编号，请重试这条记录")
	}
	return response.Candidate, nil
}

// LibraryCandidateDetail 读取候选人最小身份和版本，用于标记已保存时核实权限。
func (c *Client) LibraryCandidateDetail(ctx context.Context, token, id string) (CandidateSaveResult, error) {
	var response struct {
		Candidate CandidateSaveResult `json:"candidate"`
	}
	err := c.do(ctx, http.MethodGet, "/api/candidates/"+url.PathEscape(id), token, nil, &response)
	if err == nil && response.Candidate.ID != id {
		err = fmt.Errorf("云端没有确认这份简历的访问权限")
	}
	return response.Candidate, err
}

// AddLibraryNote 追加带固定幂等键的独立备注，重复重试不新增第二条备注。
func (c *Client) AddLibraryNote(ctx context.Context, token, candidateID, content, key string) error {
	request := struct {
		Content string `json:"content"`
		Key     string `json:"idempotency_key"`
	}{Content: content, Key: key}
	return c.do(ctx, http.MethodPost, "/api/candidates/"+url.PathEscape(candidateID)+"/notes", token, request, nil)
}

// AddLibraryRecommendation 创建待推荐的人工岗位关系，禁止触发自动岗位任务。
func (c *Client) AddLibraryRecommendation(ctx context.Context, token, candidateID, positionID, key string) error {
	request := struct {
		PositionID string `json:"position_id"`
		Stage      string `json:"stage"`
		Content    string `json:"content"`
		Key        string `json:"idempotency_key"`
	}{PositionID: positionID, Stage: "draft", Content: "", Key: key}
	return c.do(ctx, http.MethodPost, "/api/candidates/"+url.PathEscape(candidateID)+"/recommendations", token, request, nil)
}
