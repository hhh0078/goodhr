// Package cloud 文件作用：读取账号简历下载设置，查询重复线索并发送幂等站内消息，不传原件或截图。
package cloud

import (
	"context"
	"fmt"
	"net/http"
)

// ResumeLibrarySettings 表示账号级下载导入开关。
type ResumeLibrarySettings struct {
	AutoImportDownloads bool `json:"auto_import_downloads"`
}

// ResumeDownloadSettings 读取云端明确返回的开关，接口不可用时不擅自开始付费导入。
func (c *Client) ResumeDownloadSettings(ctx context.Context, token string) (ResumeLibrarySettings, error) {
	var response struct {
		Settings *struct {
			AutoImportDownloads *bool `json:"auto_import_downloads"`
		} `json:"settings"`
	}
	err := c.do(ctx, http.MethodGet, "/api/resume-library/settings", token, nil, &response)
	if err != nil {
		return ResumeLibrarySettings{}, err
	}
	if response.Settings == nil || response.Settings.AutoImportDownloads == nil {
		return ResumeLibrarySettings{}, fmt.Errorf("云端下载开关尚未确认，请更新后重新连接")
	}
	return ResumeLibrarySettings{AutoImportDownloads: *response.Settings.AutoImportDownloads}, nil
}

// LibraryDuplicateResult 只保留有无重复，不向下载流程传递其他人的资料。
type LibraryDuplicateResult struct {
	Matches []struct {
		MatchType string `json:"match_type"`
	} `json:"matches"`
	HiddenMatch *bool `json:"hidden_match"`
}

// LibraryCandidateHasDuplicate 沿用云端查重接口，精确、可能和无权限匹配都不能自动覆盖。
func (c *Client) LibraryCandidateHasDuplicate(ctx context.Context, token string, candidate LibraryCandidate) (bool, error) {
	var result LibraryDuplicateResult
	err := c.do(ctx, http.MethodPost, "/api/candidates/duplicates", token, candidate, &result)
	if err != nil {
		return false, err
	}
	if result.Matches == nil || result.HiddenMatch == nil {
		return false, fmt.Errorf("云端查重尚未确认，请从草稿核对后保存")
	}
	return *result.HiddenMatch || len(result.Matches) > 0, nil
}

// PushResumeMessage 发送当前登录账号的幂等站内通知，重复请求不会重复提醒。
func (c *Client) PushResumeMessage(ctx context.Context, token, title, body, actionURL, key string) error {
	request := struct {
		Title     string `json:"title"`
		Body      string `json:"body"`
		Type      string `json:"type"`
		ActionURL string `json:"action_url,omitempty"`
		Key       string `json:"idempotency_key"`
	}{title, body, "resume_import", actionURL, key}
	return c.do(ctx, http.MethodPost, "/api/messages/push", token, request, nil)
}
