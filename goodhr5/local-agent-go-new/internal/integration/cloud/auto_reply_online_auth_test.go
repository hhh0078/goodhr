// Package cloud 本文件通过真实Client和隔离HTTP服务核验在线资料只由服务器权威流程携带运行凭证，普通客户端不获得该凭证。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// onlineHeaderAuthority 保存当前服务器投影的最新实体，使公开Save方法的HTTP结果和权威缓存写入都走真实客户端路径。
type onlineHeaderAuthority struct {
	version int64
	data    map[string]storage.AutoReplyStateChange
}

// Load 返回本测试的权威快照，不把本机普通SQLite当作服务器凭据来源。
func (a *onlineHeaderAuthority) Load(_ context.Context, known *int64) (storage.AutoReplyStateSnapshot, bool, error) {
	snapshot := storage.AutoReplyStateSnapshot{Version: a.version}
	for _, row := range a.data {
		snapshot.Entities = append(snapshot.Entities, row)
	}
	return snapshot, known != nil && *known == a.version, nil
}

// Commit 按版本替换已写实体，重复读取不会加载旧副本或重建候选人。
func (a *onlineHeaderAuthority) Commit(_ context.Context, expected int64, changes []storage.AutoReplyStateChange) (int64, error) {
	if expected != a.version {
		return 0, errors.New("测试投影版本冲突")
	}
	for _, change := range changes {
		key := change.Kind + "|" + change.ID
		if change.Deleted {
			delete(a.data, key)
		} else {
			a.data[key] = change
		}
	}
	a.version++
	return a.version, nil
}

// TestOnlineProfileClientCarriesRuntimeProofOnlyWithServerAuthority 验证公开Save接口在服务器模式补运行凭据，普通客户端及其它接口不能收到该凭据。
func TestOnlineProfileClientCarriesRuntimeProofOnlyWithServerAuthority(t *testing.T) {
	const service = "synthetic-online-runtime-proof"
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", service)
	for _, serverMode := range []bool{true, false} {
		t.Run(map[bool]string{true: "server", false: "web"}[serverMode], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/api/auto-reply/agent/online-profile" {
					if r.Header.Get("X-GoodHR-Runtime-Service") != "" {
						t.Error("服务器运行凭证扩散到了其它接口")
					}
					_, _ = w.Write([]byte(`{}`))
					return
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-user-session" || r.Header.Get("X-GoodHR-Machine-ID") != "synthetic-machine" {
					t.Error("运行凭证替换了原登录或设备身份")
				}
				if !serverMode {
					if r.Header.Get("X-GoodHR-Runtime-Service") != "" {
						t.Error("普通客户端携带了服务器凭证")
					}
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"code":"RUNTIME_AUTH_REQUIRED","message":"只允许服务器流程"}`))
					return
				}
				if r.Header.Get("X-GoodHR-Runtime-Service") != service {
					t.Error("真实Client在线保存漏带服务器运行凭证")
				}
				_ = json.NewEncoder(w).Encode(AutoReplyOnlineProfileResult{ProfileKey: strings.Repeat("a", 64), MatchStatus: "temporary", Candidate: StructuredCandidate{CandidateName: "合成人选"}})
			}))
			defer server.Close()
			c := New(server.URL)
			cred := AgentCredentials{Token: "synthetic-user-session", MachineID: "synthetic-machine"}
			conversationID := "synthetic-conversation"
			if serverMode {
				store, err := storage.OpenAutoReplyScratch(&onlineHeaderAuthority{data: map[string]storage.AutoReplyStateChange{}})
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				c.EnableLocalAutoReply(store, t.TempDir())
				c.localAutoOwners.Store(autoReplyDigest(cred.Token), "synthetic-server-user")
				conversation, err := c.SaveAutoReplyConversation(context.Background(), cred, AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "account", PlatformThreadID: "thread", PositionID: "position"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = c.EnsureLocalAutoReplyCandidate(context.Background(), cred, conversation); err != nil {
					t.Fatal(err)
				}
				conversationID = conversation.ID
			}
			_, err := c.SaveAutoReplyOnlineProfile(context.Background(), cred, AutoReplyOnlineProfileInput{PositionID: "position", ConversationID: conversationID, PlatformID: "boss", PlatformAccountID: "account", PlatformCandidateID: "synthetic-native", Profile: model.AutoReplyOnlineProfile{CandidateName: "合成人选", SourceKey: "online-image:synthetic", RawText: "合成在线资料"}})
			if serverMode && err != nil {
				t.Fatalf("服务器真实在线保存失败：%v", err)
			}
			if !serverMode {
				var api *APIError
				if !errors.As(err, &api) || api.StatusCode != http.StatusForbidden {
					t.Fatalf("普通客户端没有留在未授权边界：%v", err)
				}
			}
			if err := c.doWithMachineID(context.Background(), http.MethodGet, "/api/config/effective-ai", cred.Token, cred.MachineID, nil, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
