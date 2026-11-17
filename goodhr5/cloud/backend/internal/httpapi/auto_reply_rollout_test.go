// 本文件验证自动回复灰度关闭的执行边界、旧任务收尾及已有配置编辑，不依赖招聘浏览器或生产数据。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// assertAutoReplyRolloutResponse 核对所有执行入口使用同一个灰度错误码和明确文案。
func assertAutoReplyRolloutResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusForbidden || body.Error.Code != autoReplyRolloutCode || body.Error.Message != autoReplyRolloutMessage {
		t.Fatalf("灰度响应不一致：status=%d body=%s err=%v", w.Code, w.Body.String(), err)
	}
}

// TestAutoReplyRolloutPlatformPolicy 验证只开放三个平台，空值、未知平台及其余平台均拒绝。
func TestAutoReplyRolloutPlatformPolicy(t *testing.T) {
	for _, id := range []string{"boss", "liepin", "zhaopin", " BOSS "} {
		if !autoReplyPlatformOpen(id) {
			t.Fatalf("应开放的平台被拦截：%q", id)
		}
	}
	for _, id := range []string{"", "hliepin", "51job", "unknown", "boss.example"} {
		w := httptest.NewRecorder()
		if !rejectAutoReplyRollout(w, id) {
			t.Fatalf("未开放平台被放行：%q", id)
		}
		assertAutoReplyRolloutResponse(t, w)
	}
}

// TestAutoReplyRolloutRuntimeUsesOwnedPosition 验证启动核对本人岗位平台，伪造平台名称不能转发执行。
func TestAutoReplyRolloutRuntimeUsesOwnedPosition(t *testing.T) {
	f, s, machine := newRolloutHTTPFixture(t)
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1); w.WriteHeader(http.StatusAccepted) }))
	defer upstream.Close()
	t.Setenv("GOODHR_AUTO_REPLY_RUNTIME_URL", upstream.URL)
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "synthetic-runtime-service")
	for _, id := range []string{"boss", "liepin", "zhaopin", "hliepin", "51job", ""} {
		p := s.positions.(*MemoryPositionStore).positions[f.pos]
		p.PlatformID = id
		s.positions.(*MemoryPositionStore).positions[f.pos] = p
		body, _ := json.Marshal(map[string]string{"position_id": f.pos, "platform_id": id})
		r := httptest.NewRequest(http.MethodPost, "/api/auto-reply/runtime/start", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer rollout-web-token")
		r.Header.Set("X-GoodHR-Machine-ID", machine)
		w := httptest.NewRecorder()
		previous := forwarded.Load()
		s.runtimeProxy(w, r)
		if autoReplyPlatformOpen(id) {
			if w.Code != http.StatusAccepted || forwarded.Load() != previous+1 {
				t.Fatalf("开放平台未转发：%q %d %s", id, w.Code, w.Body.String())
			}
		} else {
			assertAutoReplyRolloutResponse(t, w)
			if forwarded.Load() != previous {
				t.Fatal("未开放平台触发上游")
			}
		}
	}
	p := s.positions.(*MemoryPositionStore).positions[f.pos]
	p.PlatformID = "hliepin"
	s.positions.(*MemoryPositionStore).positions[f.pos] = p
	r := httptest.NewRequest(http.MethodPost, "/api/auto-reply/runtime/start", strings.NewReader(`{"position_id":"`+f.pos+`","platform_id":"boss"}`))
	r.Header.Set("Authorization", "Bearer rollout-web-token")
	r.Header.Set("X-GoodHR-Machine-ID", machine)
	w := httptest.NewRecorder()
	s.runtimeProxy(w, r)
	assertAutoReplyRolloutResponse(t, w)
}

// newRolloutHTTPFixture 复用真实隔离资料构造已登录并绑定设备的账号，不改变测试库之外的数据。
func newRolloutHTTPFixture(t *testing.T) (editableFactFixture, *AutoReplyService, string) {
	t.Helper()
	f := newEditableFactFixture(t)
	const email = "fact-hr@example.test"
	const machine = stableAgentMachineIDPrefix + "rollout-test"
	auth := NewMemoryAuthStore()
	if err := auth.SaveSession("rollout-web-token", Session{Email: email}, time.Hour); err != nil {
		t.Fatal(err)
	}
	tenants := NewMemoryTenantStore()
	tenants.tenants[f.tenant] = Tenant{ID: f.tenant, OwnerEmail: email}
	tenants.members[f.tenant] = []TenantMember{{Email: email, Role: "admin", Status: "active"}}
	agents := NewMemoryAgentStore()
	if _, err := agents.SaveBinding(AgentBinding{UserEmail: email, MachineID: machine}); err != nil {
		t.Fatal(err)
	}
	positions := NewMemoryPositionStore()
	positions.positions[f.pos] = Position{ID: f.pos, UserEmail: email, PlatformID: "liepin", Name: "开发工程师"}
	s := &AutoReplyService{
		auth: &AuthService{store: auth, superAdmins: map[string]struct{}{email: {}}}, store: f.store,
		tenants: tenants, agents: agents, positions: positions, subscriptions: NewMemorySubscriptionStore(), systemConfigs: NewMemorySystemConfigStore(),
	}
	return f, s, machine
}

// TestAutoReplyRolloutKeepsAuthenticatedDrain 验证会员到期后仍可停止、查询和提交回执，且继续核对身份与设备绑定。
func TestAutoReplyRolloutKeepsAuthenticatedDrain(t *testing.T) {
	f, s, machine := newRolloutHTTPFixture(t)
	s.subscriptions.(*MemorySubscriptionStore).items["fact-hr@example.test"] = Subscription{MemberType: memberTypeMax, ExpiresAt: time.Now().Add(-time.Hour)}
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		if r.Header.Get("X-GoodHR-Runtime-Service") != "synthetic-runtime-service" || r.Header.Get("X-GoodHR-Runtime-Tenant") != f.tenant || r.Header.Get("X-GoodHR-Runtime-User") != f.user || r.Header.Get("X-GoodHR-Machine-ID") != machine {
			t.Error("代理没有沿用已核验的身份和设备")
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	}))
	defer upstream.Close()
	t.Setenv("GOODHR_AUTO_REPLY_RUNTIME_URL", upstream.URL)
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "synthetic-runtime-service")
	for _, action := range []string{"stop", "status", "complete"} {
		r := httptest.NewRequest(http.MethodPost, "/api/auto-reply/runtime/"+action, strings.NewReader(`{"run_id":"old-run"}`))
		r.Header.Set("Authorization", "Bearer rollout-web-token")
		r.Header.Set("X-GoodHR-Machine-ID", machine)
		r.Header.Set("X-GoodHR-Runtime-User", "伪造的上游用户")
		w := httptest.NewRecorder()
		s.runtimeProxy(w, r)
		if w.Code != http.StatusAccepted {
			t.Fatal("灰度关闭破坏旧任务收尾", action, w.Code, w.Body.String())
		}
	}
	for _, authorized := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodPost, "/api/auto-reply/runtime/complete", strings.NewReader(`{}`))
		if authorized {
			r.Header.Set("Authorization", "Bearer rollout-web-token")
		}
		r.Header.Set("X-GoodHR-Machine-ID", "未绑定的设备")
		w := httptest.NewRecorder()
		s.runtimeProxy(w, r)
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
			t.Fatal("停止或回执入口绕过了登录绑定校验", w.Code, w.Body.String())
		}
	}
	if forwarded.Load() != 3 {
		t.Fatal("无权限请求被转发", forwarded.Load())
	}
}

// TestAutoReplyRolloutConfigKeepsExistingEdits 验证历史开启配置可编辑和关闭，任何账号不能从关闭重新开启。
func TestAutoReplyRolloutUnsupportedConfigKeepsExistingEdits(t *testing.T) {
	f, s, _ := newRolloutHTTPFixture(t)
	if _, err := f.db.Exec("UPDATE positions SET platform_id='hliepin' WHERE id=$1", f.pos); err != nil {
		t.Fatal(err)
	}
	p := s.positions.(*MemoryPositionStore).positions[f.pos]
	p.PlatformID = "hliepin"
	s.positions.(*MemoryPositionStore).positions[f.pos] = p
	const email = "fact-hr@example.test"
	s.subscriptions.(*MemorySubscriptionStore).items[email] = Subscription{MemberType: memberTypeMax, ExpiresAt: time.Now().Add(time.Hour)}
	cfg, err := f.store.GetPositionAutoReplyConfig(context.Background(), f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	request := func(body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPut, "/api/auto-reply/positions/"+f.pos+"/config", strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer rollout-web-token")
		w := httptest.NewRecorder()
		s.Position(w, r)
		return w
	}
	cfg.PositionDescription = "灰度期间仍可维护的岗位正文"
	if w := request(cfg); w.Code != 200 {
		t.Fatal("历史已开启岗位的普通编辑被误拦", w.Code, w.Body.String())
	}
	cfg, _ = f.store.GetPositionAutoReplyConfig(context.Background(), f.tenant, f.pos)
	if !cfg.Enabled || cfg.PositionDescription != "灰度期间仍可维护的岗位正文" {
		t.Fatal("编辑改变了已有开启设置或丢失正文")
	}
	cfg.Enabled = false
	if w := request(cfg); w.Code != 200 {
		t.Fatal("历史开关不能关闭", w.Code, w.Body.String())
	}
	cfg, _ = f.store.GetPositionAutoReplyConfig(context.Background(), f.tenant, f.pos)
	cfg.Enabled = true
	for _, member := range []string{memberTypeMax, memberTypePlus} {
		s.subscriptions.(*MemorySubscriptionStore).items[email] = Subscription{MemberType: member, ExpiresAt: time.Now().Add(time.Hour)}
		assertAutoReplyRolloutResponse(t, request(cfg))
	}
	// 旧客户端没有版本字段也必须被同一条行锁保护，而不是绕过新前端的提示。
	raw, _ := json.Marshal(cfg)
	var old map[string]any
	_ = json.Unmarshal(raw, &old)
	delete(old, "version")
	assertAutoReplyRolloutResponse(t, request(old))
	current, err := f.store.GetPositionAutoReplyConfig(context.Background(), f.tenant, f.pos)
	if err != nil || current.Enabled || current.Version != cfg.Version || current.PositionDescription != cfg.PositionDescription {
		t.Fatal("被拒绝的开启请求改变了配置", current, err)
	}
	var fresh string
	if err = f.db.QueryRow("INSERT INTO positions(user_id,name,platform_id) VALUES($1,'未配置岗位','51job') RETURNING id", f.user).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.SavePositionAutoReplyConfig(context.Background(), email, PositionAutoReplyConfig{TenantID: f.tenant, PositionID: fresh, CompanyProfileID: f.company, Enabled: true}, 0); !errors.Is(err, errAutoReplyRolloutClosed) {
		t.Fatal("尚无配置的岗位仍能首次开启", err)
	}
	if _, err = f.store.GetPositionAutoReplyConfig(context.Background(), f.tenant, fresh); !errors.Is(err, ErrNotFound) {
		t.Fatal("拒绝开启后仍写入了配置", err)
	}
}

// TestAutoReplyRolloutLegacyEnableChecksLockedRow 验证旧页面等待保存时，行锁内读取到关闭状态就不能重新开启。
func TestAutoReplyRolloutLegacyEnableChecksLockedRow(t *testing.T) {
	f := newEditableFactFixture(t)
	if _, err := f.db.Exec("UPDATE positions SET platform_id='hliepin' WHERE id=$1", f.pos); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := f.store.GetPositionAutoReplyConfig(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Version = 0
	block, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	var pid int
	if err = block.QueryRowContext(ctx, "SELECT pg_backend_pid() FROM positions WHERE id=$1 FOR UPDATE", f.pos).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.store.SavePositionAutoReplyConfig(ctx, "fact-hr@example.test", cfg); done <- err }()
	waitEditableFactBlocked(t, ctx, f.db, pid, done)
	if _, err = block.ExecContext(ctx, "UPDATE position_auto_reply_configs SET enabled=false,version=version+1 WHERE position_id=$1", f.pos); err != nil {
		t.Fatal(err)
	}
	if err = block.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, errAutoReplyRolloutClosed) {
		t.Fatal("旧客户端按等锁前的开启状态重新开启了岗位", err)
	}
}

// TestAutoReplyRolloutDoesNotChangeSubscriptionPolicy 验证发布闭锁没有改写原有Max、Plus套餐能力。
func TestAutoReplyRolloutDoesNotChangeSubscriptionPolicy(t *testing.T) {
	store := NewMemorySystemConfigStore()
	for _, test := range []struct {
		member string
		allow  bool
	}{{memberTypeMax, true}, {memberTypePlus, false}} {
		access, err := subscriptionAccess(store, Subscription{MemberType: test.member, ExpiresAt: time.Now().Add(time.Hour)}, time.Now())
		if err != nil || access.AllowAutoReply != test.allow {
			t.Fatal("灰度限制改变了套餐能力", test.member, access, err)
		}
	}
}

// TestAutoReplyRolloutConfigAndSnapshots 按真实岗位平台验证保存、状态及执行快照，未开放平台不能借历史开启配置执行。
func TestAutoReplyRolloutConfigAndSnapshots(t *testing.T) {
	for _, id := range []string{"boss", "liepin", "zhaopin", "hliepin", "51job"} {
		t.Run(id, func(t *testing.T) {
			f, s, machine := newRolloutHTTPFixture(t)
			s.subscriptions.(*MemorySubscriptionStore).items["fact-hr@example.test"] = Subscription{MemberType: memberTypePlus, ExpiresAt: time.Now().Add(time.Hour)}
			if _, err := f.db.Exec("UPDATE positions SET platform_id=$1 WHERE id=$2", id, f.pos); err != nil {
				t.Fatal(err)
			}
			p := s.positions.(*MemoryPositionStore).positions[f.pos]
			p.PlatformID = id
			s.positions.(*MemoryPositionStore).positions[f.pos] = p
			cfg, err := f.store.GetPositionAutoReplyConfig(context.Background(), f.tenant, f.pos)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Enabled = false
			cfg, err = f.store.SavePositionAutoReplyConfig(context.Background(), "fact-hr@example.test", cfg)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Enabled = true
			raw, _ := json.Marshal(cfg)
			req := httptest.NewRequest(http.MethodPut, "/api/auto-reply/positions/"+f.pos+"/config", strings.NewReader(string(raw)))
			req.Header.Set("Authorization", "Bearer rollout-web-token")
			w := httptest.NewRecorder()
			s.Position(w, req)
			if autoReplyPlatformOpen(id) {
				if w.Code != 200 {
					t.Fatalf("允许平台无法保存：%d %s", w.Code, w.Body.String())
				}
			} else {
				assertAutoReplyRolloutResponse(t, w)
			}
			// 注入历史 enabled=true，仅用于证明快照和状态仍独立检查平台。
			if _, err = f.db.Exec("UPDATE position_auto_reply_configs SET enabled=true WHERE position_id=$1", f.pos); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"positions?platform_id=" + id, "positions/" + f.pos + "/snapshot"} {
				req = httptest.NewRequest(http.MethodGet, "/api/auto-reply/agent/"+path, nil)
				req.Header.Set("Authorization", "Bearer rollout-web-token")
				req.Header.Set("X-GoodHR-Machine-ID", machine)
				w = httptest.NewRecorder()
				s.Agent(w, req)
				if autoReplyPlatformOpen(id) {
					if w.Code != 200 {
						t.Fatalf("允许平台没有快照：%s %d %s", path, w.Code, w.Body.String())
					}
				} else {
					assertAutoReplyRolloutResponse(t, w)
				}
			}
			req = httptest.NewRequest(http.MethodGet, "/api/auto-reply/positions/"+f.pos+"/status", nil)
			req.Header.Set("Authorization", "Bearer rollout-web-token")
			w = httptest.NewRecorder()
			s.Position(w, req)
			var state struct {
				Enabled    bool `json:"enabled"`
				Configured bool `json:"configured_enabled"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil || !state.Configured || state.Enabled != autoReplyPlatformOpen(id) {
				t.Fatalf("运行状态未按平台开放：%d %s", w.Code, w.Body.String())
			}
		})
	}
}
