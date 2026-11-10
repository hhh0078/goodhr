// 本文件验证在线资料HTTP入口同时需要服务凭证、已登录设备、岗位与真实会话归属。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestOnlineProfileHTTPRequiresAllScopes 验证普通网页、错设备和串候选都不能写库，完整授权才进入正式岗位规则。
func TestOnlineProfileHTTPRequiresAllScopes(t *testing.T) {
	f, store, in := onlineProfileFixture(t)
	in.MachineID = stableAgentMachineIDPrefix + "online-test"
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{machine_id}',to_jsonb($1::text)) WHERE kind='runtime_commands' AND id='online-read'`, in.MachineID); err != nil {
		t.Fatal(err)
	}
	const token = "online-synthetic-web-session"
	auth := NewMemoryAuthStore()
	if err := auth.SaveSession(token, Session{Email: in.Scope.UserEmail}, time.Hour); err != nil {
		t.Fatal(err)
	}
	tenants := NewMemoryTenantStore()
	tenants.tenants[f.tenant] = Tenant{ID: f.tenant, OwnerEmail: in.Scope.UserEmail}
	tenants.members[f.tenant] = []TenantMember{{Email: in.Scope.UserEmail, Role: "admin", Status: "active"}}
	positions := NewMemoryPositionStore()
	positions.positions[f.pos] = Position{ID: f.pos, UserEmail: in.Scope.UserEmail, PlatformID: "boss", Name: "测试岗位", CommonConfig: map[string]any{"output_structured_resume": true}}
	agents := NewMemoryAgentStore()
	if _, err := agents.SaveBinding(AgentBinding{UserEmail: in.Scope.UserEmail, MachineID: in.MachineID}); err != nil {
		t.Fatal(err)
	}
	subscriptions := NewMemorySubscriptionStore()
	subscriptions.items[in.Scope.UserEmail] = Subscription{MemberType: memberTypeMax, ExpiresAt: time.Now().Add(time.Hour)}
	s := NewAutoReplyService(&AuthService{store: auth, superAdmins: map[string]struct{}{in.Scope.UserEmail: {}}}, f.store, tenants, positions, nil, store, subscriptions, NewMemorySystemConfigStore(), agents, nil)
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "online-synthetic-service-secret")
	var saved autoReplyOnlineProfileResult
	for _, kind := range []string{"no-service", "wrong-device", "wrong-person", "authorized"} {
		body := in.Request
		if kind == "wrong-person" {
			body.PlatformCandidateID = "other-person"
		}
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/online-profile", bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GoodHR-Machine-ID", in.MachineID)
		if kind != "no-service" {
			request.Header.Set("X-GoodHR-Runtime-Service", "online-synthetic-service-secret")
		}
		if kind == "wrong-device" {
			request.Header.Set("X-GoodHR-Machine-ID", stableAgentMachineIDPrefix+"other-device")
		}
		response := httptest.NewRecorder()
		s.Agent(response, request)
		if kind == "authorized" {
			var result autoReplyOnlineProfileResult
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.CandidateID == "" {
				t.Fatalf("完整授权未成功：%d %s", response.Code, response.Body.String())
			}
			saved = result
		} else if response.Code != 403 {
			t.Fatalf("%s没有被拒绝：%d %s", kind, response.Code, response.Body.String())
		}
	}
	cacheOnlineFixture(t, f, in, saved)
	newDevice := stableAgentMachineIDPrefix + "new-bound-device"
	if _, err := agents.SaveBinding(AgentBinding{UserEmail: in.Scope.UserEmail, MachineID: newDevice}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(in.Request)
	request := httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/online-profile", bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GoodHR-Machine-ID", newDevice)
	request.Header.Set("X-GoodHR-Runtime-Service", "online-synthetic-service-secret")
	response := httptest.NewRecorder()
	s.Agent(response, request)
	var resumed autoReplyOnlineProfileResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &resumed) != nil || resumed.CandidateID != saved.CandidateID {
		t.Fatalf("已重新绑定设备不能复用同一精确缓存：%d %s", response.Code, response.Body.String())
	}
}

// TestOnlineProfileHTTPUsesActualConversationPosition 验证从A岗位运行的任务处理B会话时，API只接受B岗位命令证明与B岗位请求。
func TestOnlineProfileHTTPUsesActualConversationPosition(t *testing.T) {
	f, store, in := onlineProfileFixture(t)
	in.MachineID = stableAgentMachineIDPrefix + "multiple-position"
	var startup string
	if err := f.db.QueryRow(`INSERT INTO positions(user_id,name) VALUES($1,'启动岗位A') RETURNING id::text`, f.user).Scan(&startup); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{machine_id}',to_jsonb($1::text)) WHERE kind='runtime_commands' AND id='online-read'`, in.MachineID); err != nil {
		t.Fatal(err)
	}
	const token = "multiple-position-session"
	auth := NewMemoryAuthStore()
	if err := auth.SaveSession(token, Session{Email: in.Scope.UserEmail}, time.Hour); err != nil {
		t.Fatal(err)
	}
	tenants := NewMemoryTenantStore()
	tenants.tenants[f.tenant] = Tenant{ID: f.tenant, OwnerEmail: in.Scope.UserEmail}
	tenants.members[f.tenant] = []TenantMember{{Email: in.Scope.UserEmail, Role: "admin", Status: "active"}}
	positions := NewMemoryPositionStore()
	for _, id := range []string{startup, f.pos} {
		positions.positions[id] = Position{ID: id, UserEmail: in.Scope.UserEmail, PlatformID: "boss", CommonConfig: map[string]any{"output_structured_resume": true}}
	}
	agents := NewMemoryAgentStore()
	if _, err := agents.SaveBinding(AgentBinding{UserEmail: in.Scope.UserEmail, MachineID: in.MachineID}); err != nil {
		t.Fatal(err)
	}
	subscriptions := NewMemorySubscriptionStore()
	subscriptions.items[in.Scope.UserEmail] = Subscription{MemberType: memberTypeMax, ExpiresAt: time.Now().Add(time.Hour)}
	service := NewAutoReplyService(&AuthService{store: auth, superAdmins: map[string]struct{}{in.Scope.UserEmail: {}}}, f.store, tenants, positions, nil, store, subscriptions, NewMemorySystemConfigStore(), agents, nil)
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "multiple-position-service")
	for _, mode := range []string{"wrong-command", "wrong-request", "actual-position"} {
		body := in.Request
		proofPosition := f.pos
		if mode == "wrong-command" {
			proofPosition = startup
		}
		if mode == "wrong-request" {
			body.PositionID = startup
		}
		if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{position_id}',to_jsonb($1::text)) WHERE kind='runtime_commands' AND id='online-read'`, proofPosition); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/online-profile", bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GoodHR-Machine-ID", in.MachineID)
		request.Header.Set("X-GoodHR-Runtime-Service", "multiple-position-service")
		response := httptest.NewRecorder()
		service.Agent(response, request)
		if mode == "actual-position" {
			var saved autoReplyOnlineProfileResult
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &saved) != nil || saved.CandidateID == "" {
				t.Fatalf("真实B岗位命令证据未被API接受：%d %s", response.Code, response.Body.String())
			}
		} else if response.Code != 403 {
			t.Fatalf("%s被错当成B岗位有效证据：%d %s", mode, response.Code, response.Body.String())
		}
	}
}
