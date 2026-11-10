// Package cloudserver 本文件组装服务器上的自动回复流程、设备长轮询和 PG 租约，设备仅返回已检查的浏览器操作结果。
package cloudserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// Service 管理当前服务器进程的运行句柄，数据库中的租约决定哪个进程可操作设备。
type Service struct {
	starting            map[string]bool
	host, secret, files string
	mu                  sync.Mutex
	sessions            map[string]*activeRun
	shuttingDown        bool
	done                sync.WaitGroup
}
type runScope struct{ tenant, user, machine string }
type activeRun struct {
	failureMu                                          sync.Mutex
	failureSequence                                    int64
	failureNotices                                     []cloud.AutoReplyFailureNotice
	currentStep, errorMessage                          string
	analysis                                           shared.AnalysisStatus
	conversation                                       string
	mu                                                 sync.Mutex
	id                                                 string
	scope                                              runScope
	cred                                               cloud.AgentCredentials
	client                                             *cloud.Client
	store                                              *storage.Store
	broker                                             *replybridge.Broker
	cancel                                             context.CancelFunc
	leaseKey, legacyLeaseKey, task, platform, position string
	status                                             string
	stats                                              shared.Stats
	lastPoll                                           time.Time
}
type runtimeRequest struct {
	AfterFailure      *int64                  `json:"after_failure,omitempty"`
	RunID             string                  `json:"run_id"`
	TaskID            string                  `json:"task_id"`
	PositionID        string                  `json:"position_id"`
	MachineID         string                  `json:"machine_id"`
	PlatformID        string                  `json:"platform_id"`
	PlatformAccountID string                  `json:"platform_account_id"`
	Target            *shared.AutoReplyTarget `json:"auto_reply_target,omitempty"`
	Result            replybridge.Result      `json:"result"`
}

// New 创建一个仅接收已认证云后端代理请求的编排服务。
func New(backendURL, secret, files string) (*Service, error) {
	if strings.TrimSpace(backendURL) == "" || len(secret) < 16 || !filepath.IsAbs(files) {
		return nil, fmt.Errorf("请配置云后端地址、服务凭证和服务器附件卷绝对目录")
	}
	if err := os.MkdirAll(files, 0700); err != nil {
		return nil, err
	}
	return &Service{host: strings.TrimRight(backendURL, "/"), secret: secret, files: files, sessions: map[string]*activeRun{}, starting: map[string]bool{}}, nil
}

// ServeHTTP 校验内部代理身份和租户、用户、设备边界，再执行启动、停止或单条命令收发。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" && r.Method == http.MethodGet {
		output(w, 200, map[string]any{"ok": true})
		return
	}
	if r.Method != "POST" || !strings.HasPrefix(r.URL.Path, "/api/auto-reply/runtime/") {
		http.NotFound(w, r)
		return
	}
	scope := runScope{r.Header.Get("X-GoodHR-Runtime-Tenant"), r.Header.Get("X-GoodHR-Runtime-User"), r.Header.Get("X-GoodHR-Machine-ID")}
	bearer := r.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-GoodHR-Runtime-Service")), []byte(s.secret)) != 1 || scope.tenant == "" || scope.user == "" || scope.machine == "" || !strings.HasPrefix(bearer, "Bearer ") {
		output(w, 403, map[string]any{"ok": false, "error": "需要已绑定的设备与登录账号"})
		return
	}
	var in runtimeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<20)).Decode(&in); err != nil || (in.MachineID != "" && in.MachineID != scope.machine) {
		output(w, 400, map[string]any{"ok": false, "error": "设备请求参数不完整"})
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/auto-reply/runtime/")
	if action == "retry-reports" {
		s.retryJobs(w, r.Context(), scope, strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer ")))
		return
	}
	if action == "start" {
		run, err := s.start(r.Context(), scope, strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer ")), in)
		if err != nil {
			output(w, 409, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		output(w, 200, run.state())
		return
	}
	s.mu.Lock()
	run := s.sessions[scopeKey(scope)]
	s.mu.Unlock()
	if run == nil && action == "status" && in.RunID == "" {
		output(w, 200, map[string]any{"ok": true, "running": false, "status": "idle"})
		return
	}
	if action == "next" && in.AfterFailure != nil && (run == nil || run.id != in.RunID) && len(in.RunID) == 32 {
		if _, err := hex.DecodeString(in.RunID); err == nil {
			// 内存运行已结束或服务重启时只补发权威日志小票，不恢复历史网页命令。
			var recovered struct {
				Found    bool                           `json:"found"`
				Failures []cloud.AutoReplyFailureNotice `json:"failures"`
			}
			authority := newStateAuthority(s.host, s.secret, scope.tenant, scope.user)
			_, err = authority.request(r.Context(), "failures", map[string]any{"run_id": in.RunID, "machine_id": scope.machine, "after_failure": *in.AfterFailure}, &recovered)
			if err != nil {
				output(w, 503, map[string]any{"ok": false, "error": "运行记录暂未读到，请稍后重试"})
				return
			}
			if recovered.Found {
				output(w, 200, map[string]any{"ok": true, "running": false, "command": nil, "failures": recovered.Failures})
				return
			}
		}
	}
	if run == nil || run.scope != scope || (action != "status" || in.RunID != "") && run.id != in.RunID {
		output(w, 404, map[string]any{"ok": false, "error": "没有找到这个设备的运行任务"})
		return
	}
	switch action {
	case "status":
		output(w, 200, run.state())
	case "stop":
		run.cancel()
		output(w, 200, run.state())
	case "next":
		run.mu.Lock()
		run.lastPoll = time.Now()
		running := run.status == "running"
		run.mu.Unlock()
		if !running {
			output(w, 200, run.nextPayload(nil, false, in.AfterFailure))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		cmd, err := run.broker.Next(ctx)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			output(w, 200, run.nextPayload(nil, true, in.AfterFailure))
			return
		}
		if err != nil {
			output(w, 503, map[string]any{"ok": false, "error": "命令暂未取得，请稍后重试"})
			return
		}
		output(w, 200, run.nextPayload(cmd, true, in.AfterFailure))
	case "complete":
		if err := run.broker.Complete(r.Context(), in.Result); err != nil {
			output(w, 409, map[string]any{"ok": false, "error": "这次回执未被服务器确认"})
			return
		}
		output(w, 200, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

// start 在云端验证真实岗位、账号、权限及AI设置，PG领取成功才向设备开放命令。
func (s *Service) start(ctx context.Context, scope runScope, token string, in runtimeRequest) (*activeRun, error) {
	if in.PlatformAccountID == "" || in.PlatformID == "" || in.PositionID == "" {
		return nil, fmt.Errorf("缺少岗位或招聘账号")
	}
	key := scopeKey(scope)
	s.mu.Lock()
	if s.shuttingDown || s.starting[key] {
		s.mu.Unlock()
		return nil, fmt.Errorf("服务器正在准备或关闭任务，请稍后再试")
	}
	if old := s.sessions[key]; old != nil {
		old.mu.Lock()
		running := old.status == "running"
		old.mu.Unlock()
		if running {
			s.mu.Unlock()
			if old.task == in.TaskID && old.position == in.PositionID && old.platform == in.PlatformID && old.leaseKey == accountKey(in.PlatformID, in.PlatformAccountID) {
				return old, nil
			}
			return nil, fmt.Errorf("该设备已有自动回复任务")
		}
	}
	s.starting[key] = true
	s.done.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.starting, key); s.mu.Unlock(); s.done.Done() }()
	cfg, err := platform.LoadConfig(in.PlatformID)
	if err != nil {
		return nil, fmt.Errorf("当前招聘平台没有自动回复能力")
	}
	client := cloud.New(s.host)
	session, err := client.ValidateSession(ctx, token)
	if err != nil || session.UserID != scope.user || session.SessionKind == "browser_extension" {
		return nil, fmt.Errorf("无法确认这次登录的用户")
	}
	snapshot, err := client.AutoReplySnapshot(ctx, cloud.AgentCredentials{Token: token, MachineID: scope.machine}, in.PositionID)
	if err != nil || snapshot.Position.PlatformID != in.PlatformID || !snapshot.Subscription.Active || !snapshot.Subscription.AllowAutoReply || !snapshot.Config.Enabled {
		return nil, fmt.Errorf("岗位自动回复未开启或账号无权使用")
	}
	modelConfig, err := client.EffectiveAI(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("未能在云端读取模型配置")
	}
	aiClient := ai.New()
	if err = aiClient.Ready(modelConfig); err != nil {
		return nil, fmt.Errorf("岗位需要可用的 AI 配置")
	}
	position, err := client.Position(ctx, token, in.PositionID)
	if err != nil {
		return nil, fmt.Errorf("未能读取云端岗位")
	}
	position.AI = modelConfig
	target, err := shared.ResolveAutoReplyTarget("auto_reply", in.Target)
	if err != nil {
		return nil, err
	}
	store, err := storage.OpenAutoReplyScratch(newStateAuthority(s.host, s.secret, scope.tenant, scope.user))
	if err != nil {
		return nil, fmt.Errorf("服务器事务运行组件未准备好")
	}
	sum := sha256.Sum256([]byte(scope.tenant + "\x00" + scope.user))
	client.EnableLocalAutoReply(store, filepath.Join(s.files, hex.EncodeToString(sum[:])))
	accountScope, e := client.RuntimeAccountScope(ctx, cloud.AgentCredentials{Token: token, MachineID: scope.machine}, in.PlatformID, in.PlatformAccountID)
	if e != nil {
		store.Close()
		return nil, fmt.Errorf("暂时无法确认招聘账号的历史运行范围，请先核对该账号的工作记录")
	}
	bytesID := make([]byte, 16)
	if _, err = rand.Read(bytesID); err != nil {
		store.Close()
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run := &activeRun{id: hex.EncodeToString(bytesID), scope: scope, client: client, store: store, cred: cloud.AgentCredentials{Token: token, MachineID: scope.machine}, leaseKey: accountKey(in.PlatformID, in.PlatformAccountID), task: in.TaskID, platform: in.PlatformID, position: in.PositionID, status: "running", currentStep: "initialize_auto_reply_page", lastPoll: time.Now(), cancel: cancel}
	legacy := strings.TrimSpace(in.PlatformAccountID)
	if accountScope != shared.AutoReplyAccountKey(in.PlatformID, in.PlatformAccountID) {
		legacy = accountScope
	}
	if legacy != shared.AutoReplyAccountKey(in.PlatformID, in.PlatformAccountID) {
		run.legacyLeaseKey = rawAccountLeaseKey(in.PlatformID, legacy)
	}
	if err = run.updateLease(ctx, "claim"); err != nil {
		cancel()
		store.Close()
		return nil, fmt.Errorf("服务器未能领取该招聘账号；请确认没有其他设备正在运行")
	}
	run.broker = replybridge.NewBroker(run.persistCommand)
	run.broker.RunID, run.broker.PlatformID = run.id, in.PlatformID
	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		cancel()
		_ = run.updateLease(ctx, "stopped")
		store.Close()
		return nil, fmt.Errorf("服务正在关闭")
	}
	s.sessions[key] = run
	s.done.Add(1)
	s.mu.Unlock()
	prepared := shared.PreparedTask{Request: shared.StartRequest{TaskID: run.task, PositionID: in.PositionID, TaskType: "auto_reply", Token: token, PlatformAccountID: accountScope, AutoReplyTarget: target}, MachineID: scope.machine, Session: session, Platform: cfg, Position: position, Subscription: cloud.Subscription{Active: snapshot.Subscription.Active, AllowAutoReply: snapshot.Subscription.AllowAutoReply}}
	go func() { defer s.done.Done(); run.execute(runCtx, prepared, aiClient) }()
	return run, nil
}

// execute 将 AI、简历、推荐与浏览器 RPC 串入服务器工作流；进程退出时停止续租。
func (r *activeRun) execute(ctx context.Context, prepared shared.PreparedTask, aiClient *ai.Client) {
	defer r.store.Close()
	var stats shared.Stats
	status := "failed"
	defer func() {
		r.cancel()
		c, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		r.mu.Lock()
		if r.errorMessage != "" {
			status = "failed"
		} else if status == "failed" {
			r.errorMessage = safeRunFailure(r.currentStep)
		}
		r.stats = stats
		r.mu.Unlock()
		_ = r.updateLease(c, status)
		r.mu.Lock()
		r.status = status
		r.mu.Unlock()
	}()
	temp, err := os.MkdirTemp("", "goodhr-server-browser-")
	if err != nil {
		return
	}
	defer os.RemoveAll(temp)
	if target := prepared.Request.AutoReplyTarget; target != nil {
		// 单人任务启动即记录历史封印的观察时间，候选人下一次真实来信可以从新边界进入。
		_, err = r.client.AutoReplyDeletionBoundary(ctx, r.cred, cloud.AutoReplyCandidateLookup{PlatformID: r.platform, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: target.PlatformThreadID, PlatformCandidateID: target.PlatformCandidateID})
		if err != nil {
			return
		}
	}
	platformRuntime := replybridge.NewPlatform(r.platform, r.broker.Call, temp)
	logger := &runLogger{run: r}
	responder := &auto_reply.AIResponder{AI: aiClient, Cloud: r.client, Logger: logger}
	// 本机及旧服务器OCR注入停用，源码serverOCR保留；扫描附件统一复用服务器AI图片识别。
	flow := auto_reply.Flow{Cloud: r.client, Store: r.store, Responder: responder, Logger: logger}
	background := auto_reply.NewBackground(r.client, responder, r.scope.machine)
	background.Bind(r.scope.user, r.cred.Token)
	jobCtx, stop := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); background.Run(jobCtx) }()
	go func() {
		defer wg.Done()
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-t.C:
				r.mu.Lock()
				absent := time.Since(r.lastPoll) > 2*time.Minute
				r.mu.Unlock()
				if absent {
					r.fail("设备已断开，请连接本机程序后重新启动。")
					return
				}
				if jobCtx.Err() == nil && r.updateLease(jobCtx, "heartbeat") != nil && jobCtx.Err() == nil {
					r.fail("云端未能确认招聘账号的运行权，请检查设备连接后重新启动。")
					return
				}
			}
		}
	}()
	stats, err = flow.RunServer(jobCtx, prepared, platformRuntime)
	if details := failurediag.Details(err); len(details) > 0 && ctx.Err() == nil {
		r.mu.Lock()
		if r.errorMessage == "" {
			r.errorMessage = failurediag.Summary(details)
		}
		r.mu.Unlock()
		// 只有源头核验过的岗位才会收到终止记录。全局扫描故障仅进入本次运行审计。
		logger.Step(r.task, "auto_reply", "server_run_stopped", "failed", time.Time{}, err)
	}
	stop()
	wg.Wait()
	if err == nil || ctx.Err() != nil {
		status = "stopped"
	}
}

// safeRunFailure 按可信阶段提供下一步，不将模型、浏览器或网络错误中的地址和凭据显示给用户。
func safeRunFailure(stage string) string {
	switch stage {
	case "initialize_auto_reply_page", "check_open_conversation", "check_open_after_scan":
		return "没能确认招聘页面，请打开已登录的招聘平台聊天页后重试。"
	case "scan_unread_conversations":
		return "没能读到会话列表，请确认招聘平台的「全部」列表可见后重试。"
	case "review_contact_list":
		return "联系人变化还没核对好，本轮没有继续回复，请查看运行记录后重试。"
	case "load_positions":
		return "岗位配置暂时读不到，请确认岗位已开启自动回复后重试。"
	case "ai", "resume_ai", "structure_resume":
		return "AI 处理未完成，请检查模型配置，再查看候选人的处理记录。"
	case "awaiting_hr_notice":
		return "待确认问题已记下，提醒还在排队；其他消息继续处理。"
	case "message_goal_review", "phone_goal_review":
		return "目标复核暂未完成，有依据的回复和新消息会继续处理。"
	case "process_conversation", "sync", "sent", "message_changed":
		return "这条沟通没能处理完，请先查看聊天记录和发送回执，再重新启动。"
	default:
		return "自动回复暂时停下了，请检查招聘页面和设备连接后重新启动。"
	}
}

// fail 标记首次内部故障后取消流程，把设备或云端故障与用户主动停止区分开。
func (r *activeRun) fail(message string) {
	r.mu.Lock()
	if r.errorMessage == "" {
		r.errorMessage = message
	}
	r.mu.Unlock()
	r.cancel()
}

// state 返回普通任务状态，不包含鉴权信息、模型输入或设备指令内容。
func (r *activeRun) state() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]any{"ok": true, "run_id": r.id, "task_id": r.task, "running": r.status == "running", "status": r.status, "stats": r.stats, "current_step": r.currentStep, "error_message": r.errorMessage, "scanned_count": r.stats.Processed, "greeted_count": r.stats.Succeeded, "skipped_count": r.stats.Skipped, "analysis": r.analysis}
}

// scopeKey 对已验证的用户和设备做稳定隔离。
func scopeKey(s runScope) string { return s.tenant + "\x00" + s.user + "\x00" + s.machine }

// accountKey 仅将当前平台与招聘账号作为互斥资源，不包含可读的候选人信息。
func accountKey(platform, account string) string {
	return rawAccountLeaseKey(platform, shared.AutoReplyAccountKey(platform, account))
}

// rawAccountLeaseKey 只作为迁移期兼容旧进程的互斥别名，新调度统一按规范键领取租约。
func rawAccountLeaseKey(platform, account string) string {
	hash := sha256.Sum256([]byte(platform + "\x00" + account))
	return "lease:" + hex.EncodeToString(hash[:])
}

// output 返回统一 JSON 状态。
func output(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Close 终止所有运行中的服务器工作流并等待已开始的事务结束。
func (s *Service) Close() {
	s.mu.Lock()
	s.shuttingDown = true
	for _, r := range s.sessions {
		r.cancel()
	}
	s.mu.Unlock()
	s.done.Wait()
}
