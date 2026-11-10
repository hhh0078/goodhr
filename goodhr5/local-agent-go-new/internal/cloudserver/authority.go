// Package cloudserver 本文件只在云端编排进程访问 PG 状态接口；用户和设备不能直接写入服务器事务。
package cloudserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

type stateAuthority struct {
	host, secret, tenant, user string
	http                       *http.Client
}

// request 仅向可信云服务地址发送单账号事务，不使用候选人输入拼接地址、凭证或租户。
func (a stateAuthority) request(ctx context.Context, action string, fields map[string]any, result any) (int, error) {
	fields["tenant_id"] = a.tenant
	fields["user_id"] = a.user
	raw, err := json.Marshal(fields)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(a.host, "/")+"/internal/auto-reply/state/"+action, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.secret)
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("无法连接云端事务服务")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 304 {
		return resp.StatusCode, nil
	}
	if resp.StatusCode != 200 {
		return resp.StatusCode, fmt.Errorf("服务器事务未提交（HTTP %d）", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
		return resp.StatusCode, fmt.Errorf("无法确认云端事务结果")
	}
	return resp.StatusCode, nil
}

// Load 恢复给定账号的最新 PG 状态，已加载版本一致时仅返回版本命中。
func (a stateAuthority) Load(ctx context.Context, known *int64) (snap storage.AutoReplyStateSnapshot, unchanged bool, err error) {
	fields := map[string]any{}
	if known != nil {
		fields["known_version"] = *known
	}
	status, err := a.request(ctx, "load", fields, &snap)
	return snap, status == 304, err
}

// Commit 使用PG账号版本作CAS，冲突和未知结果一律交给下轮从服务器恢复。
func (a stateAuthority) Commit(ctx context.Context, version int64, changes []storage.AutoReplyStateChange) (int64, error) {
	var res struct {
		Version int64 `json:"version"`
	}
	_, err := a.request(ctx, "commit", map[string]any{"expected_version": version, "changes": changes}, &res)
	return res.Version, err
}

// newStateAuthority 让云端事务请求复用连接并限制单次等待。
func newStateAuthority(host, secret, tenant, user string) stateAuthority {
	return stateAuthority{host: host, secret: secret, tenant: tenant, user: user, http: &http.Client{Timeout: 90 * time.Second}}
}
