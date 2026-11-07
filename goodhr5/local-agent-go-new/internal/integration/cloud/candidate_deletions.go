// Package cloud 本文件读取当前设备有权清理的简历删除事件，把未完成项与领取进度一起持久化。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// CandidateDeletion 只传递旧简历编号和来源摘要，不在设备间传递简历正文。
type CandidateDeletion struct {
	BoundarySourceHashes []string  `json:"boundary_source_hashes,omitempty"`
	TenantID             string    `json:"tenant_id,omitempty"`
	ContactHashes        []string  `json:"contact_hashes,omitempty"`
	DeletedAt            time.Time `json:"deleted_at,omitempty"`
	CutoffFromMigration  bool      `json:"cutoff_from_migration,omitempty"`
	Sequence             int64     `json:"sequence"`
	CandidateID          string    `json:"candidate_id"`
	SourceHashes         []string  `json:"source_hashes"`
	RequestHashes        []string  `json:"request_hashes"`
}

// IsCandidateDeleted 仅当服务端明确确认删除才停止重试，权限或网络错误仍由原流程恢复。
func IsCandidateDeleted(err error) bool {
	var reply *APIError
	return errors.As(err, &reply) && reply.StatusCode == http.StatusGone && reply.Code == "CANDIDATE_DELETED"
}

// deletionProgress 将领取水位及尚待重试的独立事件同时落盘，单份文件占用不会阻断以后其他简历的清理。
type deletionProgress struct {
	BoundaryVersion int                 `json:"boundary_version,omitempty"`
	Cursor          int64               `json:"cursor"`
	Pending         []CandidateDeletion `json:"pending,omitempty"`
}

// SyncCandidateDeletions 按服务地址和账号分别续读；断电或切号可能重放已执行步骤，清理步骤必须幂等。
func (c *Client) SyncCandidateDeletions(ctx context.Context, credentials AgentCredentials, apply func(context.Context, CandidateDeletion) error) error {
	if !c.LocalAutoReplyEnabled() || apply == nil {
		return fmt.Errorf("简历清理尚未准备好")
	}
	key := "candidate_deletions:v1:" + autoReplyDigest(c.baseURL) + ":" + c.localAutoReplyKey(credentials)
	raw, err := c.localAutoStore.GetSetting(ctx, key)
	if err != nil {
		return err
	}
	var state deletionProgress
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &state); err != nil || state.Cursor < 0 {
			return fmt.Errorf("简历清理进度无法读取")
		}
	}
	save := func() error {
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return c.localAutoStore.SetSetting(ctx, key, string(data))
	}
	var incomplete error
	old := state.Pending
	state.Pending = nil
	for _, event := range old {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = apply(ctx, event); err != nil {
			state.Pending = append(state.Pending, event)
			incomplete = err
		}
	}
	if len(old) != len(state.Pending) {
		if err = save(); err != nil {
			return err
		}
	}
	for {
		var page struct {
			BoundaryVersion int                 `json:"boundary_version"`
			OK              bool                `json:"ok"`
			Events          []CandidateDeletion `json:"events"`
			NextCursor      int64               `json:"next_cursor"`
			HasMore         bool                `json:"has_more"`
		}
		path := autoReplyAgentBasePath + "/candidate-deletions?after=" + strconv.FormatInt(state.Cursor, 10)
		err = c.doBody(ctx, http.MethodGet, path, credentials.Token, credentials.MachineID, "", nil, &page)
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return incomplete
		} // 旧云端尚不支持单份删除。
		if err != nil {
			return err
		}
		if !page.OK || page.NextCursor < state.Cursor || page.HasMore && page.NextCursor <= state.Cursor {
			return fmt.Errorf("云端没有确认简历清理进度")
		}
		if page.BoundaryVersion >= 1 && state.BoundaryVersion < 1 {
			// Agent 或云端先升级均可：新服务明确支持删除时间后，从头补收已确认事件一次。
			state.BoundaryVersion = 1
			if state.Cursor > 0 {
				state.Cursor = 0
				if err = save(); err != nil {
					return err
				}
				continue
			}
			if err = save(); err != nil {
				return err
			}
		}
		previous := state.Cursor
		for _, event := range page.Events {
			if event.Sequence <= state.Cursor || event.Sequence > page.NextCursor || event.CandidateID == "" || event.CandidateID == BuiltinCandidateID {
				return fmt.Errorf("简历清理记录不完整")
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err = apply(ctx, event); err != nil {
				state.Pending = append(state.Pending, event)
				incomplete = err
			}
			state.Cursor = event.Sequence
		}
		state.Cursor = page.NextCursor
		if state.Cursor != previous {
			if err = save(); err != nil {
				return err
			}
		}
		if !page.HasMore {
			return incomplete
		}
	}
}
