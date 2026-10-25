// Package cloud 文件作用：按完整快照异步同步岗位筛选记录，只接受明确云端版本回执。
package cloud

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"goodhr5/local-agent-go-new/internal/screening"
)

// ScreeningRecordSnapshot 是跨端合同使用的两轮完整快照，字段定义与本地持久化保持同一来源。
type ScreeningRecordSnapshot = screening.Snapshot

// ScreeningRecordReceipt 保存云端确认的记录编号和当前版本，不把HTTP成功直接当成已同步。
type ScreeningRecordReceipt struct {
	OK       bool   `json:"ok"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// SaveScreeningRecord 上传原编号及原版本快照，云端旧版本回执不能覆盖本地新结果。
func (c *Client) SaveScreeningRecord(ctx context.Context, token, positionID string, item ScreeningRecordSnapshot) (ScreeningRecordReceipt, error) {
	var receipt ScreeningRecordReceipt
	if strings.TrimSpace(positionID) == "" || strings.TrimSpace(token) == "" {
		return receipt, fmt.Errorf("筛选同步缺少岗位或登录状态")
	}
	err := c.do(ctx, http.MethodPost, "/api/positions/"+url.PathEscape(positionID)+"/screening-records", token, item, &receipt)
	if err == nil && (!receipt.OK || receipt.ID == "" || receipt.Revision < item.Revision) {
		err = fmt.Errorf("云端尚未确认本次筛选版本")
	}
	return receipt, err
}
