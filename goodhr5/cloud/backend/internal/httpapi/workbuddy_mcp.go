// 本文件负责 WorkBuddy 连接器的 MCP streamableHttp 端点：
// 提供 check_setup、get_quota、create_task、get_task_status、get_results 五个工具。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	// mcpProtocolVersion 服务端支持的 MCP 协议版本。
	mcpProtocolVersion = "2025-03-26"
	// workbuddyStartCommand 云端下发给本地程序的启动命令消息类型。
	workbuddyStartCommand = "position.start"
	// workbuddyStartAckTimeout 等待本地程序确认接收启动命令的超时时间。
	workbuddyStartAckTimeout = 15 * time.Second
)

// WorkBuddyMCPService 处理 WorkBuddy 连接器的 MCP 请求。
type WorkBuddyMCPService struct {
	auth          *AuthService
	positions     PositionStore
	tenantStore   TenantStore
	agents        AgentStore
	agentWS       *AgentWSHub
	candidates    CandidateStore
	positionLogs  PositionLogStore
	subscriptions SubscriptionStore
	systemConfigs SystemConfigStore
}

// NewWorkBuddyMCPService 创建 MCP 服务并注入业务依赖。
func NewWorkBuddyMCPService(auth *AuthService, positions PositionStore, tenantStore TenantStore, agents AgentStore, agentWS *AgentWSHub, candidates CandidateStore, positionLogs PositionLogStore, subscriptions SubscriptionStore, systemConfigs SystemConfigStore) *WorkBuddyMCPService {
	return &WorkBuddyMCPService{
		auth: auth, positions: positions, tenantStore: tenantStore, agents: agents,
		agentWS: agentWS, candidates: candidates, positionLogs: positionLogs,
		subscriptions: subscriptions, systemConfigs: systemConfigs,
	}
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"params"`
}

type mcpResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *mcpRPCError  `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeMCP 处理 MCP streamableHttp 请求（无状态 JSON 模式）。
func (s *WorkBuddyMCPService) ServeMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// 无状态模式不提供 SSE 长连接，客户端按 POST 请求-响应模式通信即可。
		writeError(w, http.StatusMethodNotAllowed, "MCP 端点只支持 POST 请求")
		return
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodPost:
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 会话校验：只接受网页会话和连接器会话，插件会话由总守卫拦截。
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="/.well-known/oauth-protected-resource"`)
		writeError(w, http.StatusUnauthorized, "连接器令牌无效或已过期，请在 WorkBuddy 重新连接")
		return
	}
	if session.Kind != "" && session.Kind != workbuddySessionKind {
		writeError(w, http.StatusForbidden, "当前会话不能使用连接器")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeRPC(w, mcpResponse{JSONRPC: "2.0", Error: &mcpRPCError{Code: -32700, Message: "请求不是合法的 JSON"}})
		return
	}
	// 通知类消息不需要响应。
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		s.writeRPC(w, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo": map[string]any{
				"name":    "goodhr-connector",
				"title":   "GoodHR 招聘助手",
				"version": "1.0.0",
			},
			"instructions": "通过这些工具查看 GoodHR 的岗位任务配置、额度，启动筛选任务并取回候选人结果。启动任务前先调用 check_setup 确认本地程序在线。",
		}})
	case "ping":
		s.writeRPC(w, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		s.writeRPC(w, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": s.toolDefinitions()}})
	case "tools/call":
		s.callTool(w, r, req, session)
	default:
		s.writeRPC(w, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpRPCError{Code: -32601, Message: fmt.Sprintf("未知方法 %s", req.Method)}})
	}
}

// writeRPC 输出单条 JSON-RPC 响应。
func (s *WorkBuddyMCPService) writeRPC(w http.ResponseWriter, resp mcpResponse) {
	writeJSON(w, http.StatusOK, resp)
}

// toolDefinitions 返回连接器全部工具定义。
func (s *WorkBuddyMCPService) toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "check_setup",
			"description": "检查 GoodHR 本地程序安装和在线状态。返回是否绑定、是否在线、版本号和下载地址。第一次使用或任务启动失败时先调用这个工具。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
		{
			"name":        "get_quota",
			"description": "查看当前账号的订阅状态、可建岗位额度（未订阅只能建 1 个岗位）和全部岗位清单（含运行状态和计数）。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
		{
			"name":        "create_task",
			"description": "启动一个已存在岗位的筛选任务。命令发给用户电脑上的本地程序，立即返回任务编号，之后用 get_task_status 轮询进度。岗位必须已开启（is_open）且本地程序在线。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"position_id": map[string]any{"type": "string", "description": "岗位编号，来自 get_quota 返回的岗位清单"},
					"task_type":   map[string]any{"type": "string", "description": "任务类型，greeting=筛选并打招呼（默认），不传则用 greeting"},
				},
				"required": []string{"position_id"},
			},
		},
		{
			"name":        "get_task_status",
			"description": "查询岗位任务的运行状态、扫描/打招呼/跳过/失败计数和最近日志。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"position_id": map[string]any{"type": "string", "description": "岗位编号"},
				},
				"required": []string{"position_id"},
			},
		},
		{
			"name":        "get_results",
			"description": "分页获取岗位已筛选出的候选人结果，包含姓名、平台、AI 评分和推荐理由。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"position_id": map[string]any{"type": "string", "description": "岗位编号"},
					"page":        map[string]any{"type": "integer", "description": "页码，从 1 开始，默认 1"},
					"page_size":   map[string]any{"type": "integer", "description": "每页数量，默认 20，最大 50"},
				},
				"required": []string{"position_id"},
			},
		},
	}
}

// callTool 分发 tools/call 调用到具体工具实现。
func (s *WorkBuddyMCPService) callTool(w http.ResponseWriter, r *http.Request, req mcpRequest, session Session) {
	args := map[string]any{}
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &args)
	}
	var (
		result map[string]any
		err    *mcpToolError
	)
	switch req.Params.Name {
	case "check_setup":
		result, err = s.toolCheckSetup(r, session)
	case "get_quota":
		result, err = s.toolGetQuota(session)
	case "create_task":
		result, err = s.toolCreateTask(session, stringArg(args, "position_id"), stringArg(args, "task_type"))
	case "get_task_status":
		result, err = s.toolGetTaskStatus(session, stringArg(args, "position_id"))
	case "get_results":
		result, err = s.toolGetResults(session, stringArg(args, "position_id"), intArg(args, "page", 1), intArg(args, "page_size", 20))
	default:
		s.writeRPC(w, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpRPCError{Code: -32602, Message: fmt.Sprintf("未知工具 %s", req.Params.Name)}})
		return
	}
	if err != nil {
		// 工具业务错误按 MCP 规范放在 result.isError，方便 AI 读取并提示用户。
		s.writeRPC(w, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content":  []map[string]any{{"type": "text", "text": err.Message}},
			"isError":  true,
			"error_code": err.Code,
		}})
		return
	}
	text, _ := json.Marshal(result)
	s.writeRPC(w, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
	}})
}

// mcpToolError 表示工具层面的业务错误。
type mcpToolError struct {
	Code    string
	Message string
}

func toolError(code, message string) *mcpToolError {
	return &mcpToolError{Code: code, Message: message}
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func intArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case float64:
		if value >= 1 {
			return int(value)
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed); err == nil && parsed >= 1 {
			return parsed
		}
	}
	return fallback
}

// toolCheckSetup 返回本地程序绑定和在线状态，以及安装包下载地址。
func (s *WorkBuddyMCPService) toolCheckSetup(r *http.Request, session Session) (map[string]any, *mcpToolError) {
	online := s.agentWS.IsOnline(session.Email)
	binding, bindErr := s.agents.CurrentBinding(session.Email)
	bound := bindErr == nil
	result := map[string]any{
		"local_program": map[string]any{
			"bound":    bound,
			"online":   online,
			"ready":    bound && online,
			"platform": "desktop",
		},
		"next_step": "本地程序已就绪，可以直接用 get_quota 查看岗位，再 create_task 启动筛选。",
	}
	if bound {
		result["local_program"].(map[string]any)["machine_id"] = binding.MachineID
		result["local_program"].(map[string]any)["agent_version"] = binding.AgentVersion
		result["local_program"].(map[string]any)["last_seen_at"] = binding.LastSeenAt.Format(time.RFC3339)
	}
	if !bound || !online {
		result["downloads"] = s.localAgentDownloads()
		result["download_page"] = workBuddyBaseURL(r) + "/download"
		if !bound {
			result["next_step"] = "还没绑定本地程序：先在 GoodHR 下载页安装本地程序并登录，之后本工具会显示在线。"
		} else {
			result["next_step"] = "本地程序已安装但没连上云端：请打开电脑上的 GoodHR 本地程序，确认它在运行。"
		}
	}
	return result, nil
}

// localAgentDownloads 从系统配置读取本地程序安装包地址。
func (s *WorkBuddyMCPService) localAgentDownloads() map[string]string {
	downloads := map[string]string{"windows": "", "macos": ""}
	if s.systemConfigs == nil {
		return downloads
	}
	cfg, err := s.systemConfigs.Get("system.onboarding_config")
	if err != nil {
		return downloads
	}
	var value struct {
		LocalAgent []map[string]any `json:"local_agent"`
	}
	if json.Unmarshal([]byte(cfg.ConfigValue), &value) != nil {
		return downloads
	}
	for _, item := range value.LocalAgent {
		platform, _ := item["platform"].(string)
		url, _ := item["url"].(string)
		if url == "" {
			continue
		}
		if strings.EqualFold(platform, "windows") || strings.EqualFold(platform, "win") {
			downloads["windows"] = url
		}
		if strings.EqualFold(platform, "macos") || strings.EqualFold(platform, "mac") || strings.EqualFold(platform, "darwin") {
			downloads["macos"] = url
		}
	}
	return downloads
}

// toolGetQuota 返回订阅状态、岗位额度和岗位清单。
func (s *WorkBuddyMCPService) toolGetQuota(session Session) (map[string]any, *mcpToolError) {
	tenantID, isAdmin := s.tenantInfo(session.Email)
	positions, err := s.positions.ListPositions(tenantID, session.Email, isAdmin)
	if err != nil {
		return nil, toolError("POSITIONS_LOAD_FAILED", "岗位清单暂时没读出来，请稍后再试")
	}
	list := make([]map[string]any, 0, len(positions))
	for _, item := range positions {
		list = append(list, map[string]any{
			"position_id": item.ID,
			"name":        item.Name,
			"platform":    item.PlatformID,
			"is_open":     item.IsOpen,
			"status":      item.Status,
			"scanned":     item.ScannedCount,
			"greeted":     item.DailyGreetedCount,
			"skipped":     item.SkippedCount,
			"failed":      item.FailedCount,
		})
	}
	subscription, err := s.subscriptions.UserSubscription(session.Email)
	if err != nil {
		return nil, toolError("SUBSCRIPTION_LOAD_FAILED", "订阅状态暂时没读出来，请稍后再试")
	}
	subscribed := subscription.MemberType != "" && time.Now().Before(subscription.ExpiresAt)
	positionLimit := 1
	if subscribed {
		positionLimit = 0 // 0 表示不限
	}
	return map[string]any{
		"subscription": map[string]any{
			"active":     subscribed,
			"member_type": subscription.MemberType,
			"expires_at": subscription.ExpiresAt.Format(time.RFC3339),
		},
		"position_limit": positionLimit,
		"position_count": len(positions),
		"positions":      list,
	}, nil
}

// toolCreateTask 校验岗位状态后把启动命令下发给本地程序，立即返回任务编号。
func (s *WorkBuddyMCPService) toolCreateTask(session Session, positionID, taskType string) (map[string]any, *mcpToolError) {
	if positionID == "" {
		return nil, toolError("POSITION_ID_REQUIRED", "position_id 不能为空，可以先用 get_quota 查看岗位清单")
	}
	if !s.agentWS.IsOnline(session.Email) {
		return nil, toolError("LOCAL_PROGRAM_OFFLINE", "本地程序不在线：请打开电脑上的 GoodHR 本地程序。第一次使用可先调用 check_setup 获取下载地址。")
	}
	tenantID, isAdmin := s.tenantInfo(session.Email)
	position, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		return nil, toolError("POSITION_NOT_FOUND", "这个岗位没有找到，可能已经被删除了。可以先用 get_quota 查看岗位清单。")
	}
	if err != nil {
		return nil, toolError("POSITION_LOAD_FAILED", "岗位信息暂时没读出来，请稍后再试")
	}
	if !position.IsOpen {
		return nil, toolError("POSITION_CLOSED", "这个岗位还没开启，请先在 GoodHR 控制台开启岗位再启动任务。")
	}
	if position.Status == "running" {
		return nil, toolError("POSITION_RUNNING", "这个岗位正在运行中，不用重复启动。可以用 get_task_status 查看进度。")
	}
	if strings.EqualFold(strings.TrimSpace(taskType), "auto_reply") {
		return nil, toolError("TASK_TYPE_UNSUPPORTED", "自动回复任务请在 GoodHR 控制台操作，连接器暂不支持。")
	}
	if strings.TrimSpace(taskType) == "" {
		taskType = "greeting"
	}
	// 启动命令等待本地程序快速确认（不等任务执行完成），超时按失败处理。
	command := AgentWSMessage{
		Type:       workbuddyStartCommand,
		PositionID: position.ID,
		Payload:    map[string]any{"position_id": position.ID, "task_type": taskType},
	}
	reply, err := s.sendStartCommand(session.Email, command)
	if err != nil {
		log.Printf("[WorkBuddyMCP] 启动命令下发失败 user=%s position=%s err=%v", session.Email, position.ID, err)
		return nil, toolError("LOCAL_PROGRAM_START_FAILED", "本地程序没有确认接收任务：请确认本地程序已更新到最新版本并保持运行。")
	}
	message := "任务已下发给本地程序，马上开始筛选。"
	if replyMsg := strings.TrimSpace(reply); replyMsg != "" {
		message = replyMsg
	}
	return map[string]any{
		"task_id":     position.ID,
		"position_id": position.ID,
		"task_type":   taskType,
		"status":      "queued",
		"message":     message,
		"next_step":   "用 get_task_status 轮询进度，任务完成后用 get_results 拉取候选人结果。",
	}, nil
}

// sendStartCommand 发送启动命令并限时等待本地程序确认。
func (s *WorkBuddyMCPService) sendStartCommand(userEmail string, command AgentWSMessage) (string, error) {
	type ackResult struct {
		message string
		err     error
	}
	done := make(chan ackResult, 1)
	go func() {
		reply, err := s.agentWS.SendCommand(userEmail, command, 1)
		if err != nil {
			done <- ackResult{err: err}
			return
		}
		message, _ := reply.Payload["message"].(string)
		done <- ackResult{message: message}
	}()
	select {
	case result := <-done:
		return result.message, result.err
	case <-time.After(workbuddyStartAckTimeout):
		return "", errors.New("local agent start ack timeout")
	}
}

// toolGetTaskStatus 返回岗位运行状态、计数和最近日志。
func (s *WorkBuddyMCPService) toolGetTaskStatus(session Session, positionID string) (map[string]any, *mcpToolError) {
	if positionID == "" {
		return nil, toolError("POSITION_ID_REQUIRED", "position_id 不能为空，可以先用 get_quota 查看岗位清单")
	}
	tenantID, isAdmin := s.tenantInfo(session.Email)
	position, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		return nil, toolError("POSITION_NOT_FOUND", "这个岗位没有找到，可能已经被删除了。")
	}
	if err != nil {
		return nil, toolError("POSITION_LOAD_FAILED", "岗位信息暂时没读出来，请稍后再试")
	}
	status := map[string]any{
		"task_id":     position.ID,
		"position_id": position.ID,
		"name":        position.Name,
		"status":      position.Status,
		"is_open":     position.IsOpen,
		"counts": map[string]any{
			"scanned": position.ScannedCount,
			"greeted": position.DailyGreetedCount,
			"skipped": position.SkippedCount,
			"failed":  position.FailedCount,
		},
	}
	if position.StartedAt != nil {
		status["started_at"] = position.StartedAt.Format(time.RFC3339)
	}
	if position.FinishedAt != nil {
		status["finished_at"] = position.FinishedAt.Format(time.RFC3339)
	}
	logs := []map[string]any{}
	if s.positionLogs != nil {
		items, _, logErr := s.positionLogs.ListPositionLogs(tenantID, session.Email, position.ID, isAdmin, PositionLogQuery{Limit: 10})
		if logErr == nil {
			for _, item := range items {
				logs = append(logs, map[string]any{
					"level":      item.Level,
					"message":    item.Message,
					"created_at": item.CreatedAt.Format(time.RFC3339),
				})
			}
		}
	}
	status["recent_logs"] = logs
	return status, nil
}

// toolGetResults 分页返回岗位候选人筛选结果。
func (s *WorkBuddyMCPService) toolGetResults(session Session, positionID string, page, pageSize int) (map[string]any, *mcpToolError) {
	if positionID == "" {
		return nil, toolError("POSITION_ID_REQUIRED", "position_id 不能为空，可以先用 get_quota 查看岗位清单")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	tenantID, isAdmin := s.tenantInfo(session.Email)
	// 先确认岗位归属，避免越权拉取他人岗位的候选人。
	if _, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin); errors.Is(err, ErrNotFound) {
		return nil, toolError("POSITION_NOT_FOUND", "这个岗位没有找到，可能已经被删除了。")
	}
	result, err := s.candidates.ListPositionCandidates(tenantID, PositionCandidateQuery{
		PositionID: positionID,
		UserEmail:  session.Email,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		return nil, toolError("RESULTS_LOAD_FAILED", "候选人结果暂时没读出来，请稍后再试")
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		candidate := map[string]any{
			"candidate_id":     item.ID,
			"name":             item.CandidateName,
			"platform":         item.PlatformID,
			"engagement_status": item.EngagementStatus,
			"work_years":       item.WorkYears,
			"education":        item.EducationLevel,
			"created_at":       item.CreatedAt.Format(time.RFC3339),
		}
		if item.AIDetailScore != nil {
			candidate["ai_score"] = *item.AIDetailScore
		}
		if strings.TrimSpace(item.AIDetailReason) != "" {
			candidate["ai_reason"] = item.AIDetailReason
		} else if strings.TrimSpace(item.AIReviewReason) != "" {
			if item.AIReviewScore != nil {
				candidate["ai_score"] = *item.AIReviewScore
			}
			candidate["ai_reason"] = item.AIReviewReason
		}
		items = append(items, candidate)
	}
	return map[string]any{
		"position_id": positionID,
		"total":       result.Total,
		"page":        result.Page,
		"page_size":   result.PageSize,
		"candidates":  items,
	}, nil
}

// tenantInfo 解析用户所属团队和管理员身份，失败时按独立用户处理。
func (s *WorkBuddyMCPService) tenantInfo(email string) (string, bool) {
	if s.tenantStore == nil {
		return "", false
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(email)
	if err != nil {
		return "", false
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, email)
	return tenant.ID, isAdmin
}
