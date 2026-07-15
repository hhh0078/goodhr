// 本文件负责维护用户首次跑通招聘任务的流程状态、事件和上报接口。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const userFlowVersion = 2

const (
	userFlowAgentDetected        = "agent_detected"
	userFlowRuntimeReady         = "runtime_ready"
	userFlowPositionCreated      = "position_created"
	userFlowTaskCreated          = "task_created"
	userFlowPlatformLogin        = "platform_login_verified"
	userFlowTaskStarted          = "task_started"
	userFlowFirstResumeProcessed = "first_resume_processed"
	userFlowFirstGreetSuccess    = "first_greet_success"
)

var userFlowStepOrder = []string{
	userFlowAgentDetected,
	userFlowRuntimeReady,
	userFlowPositionCreated,
	userFlowTaskCreated,
	userFlowPlatformLogin,
	userFlowTaskStarted,
	userFlowFirstResumeProcessed,
	userFlowFirstGreetSuccess,
}

var userFlowStepNames = map[string]string{
	userFlowAgentDetected:        "启动本地程序",
	userFlowRuntimeReady:         "安装运行组件",
	userFlowPositionCreated:      "创建岗位",
	userFlowTaskCreated:          "创建任务",
	userFlowPlatformLogin:        "登录招聘平台",
	userFlowTaskStarted:          "启动任务",
	userFlowFirstResumeProcessed: "处理首份简历",
	userFlowFirstGreetSuccess:    "首次打招呼成功",
}

// UserFlowStepState 表示一个招聘流程节点的最新状态。
type UserFlowStepState struct {
	Status          string     `json:"status"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	LastAttemptedAt *time.Time `json:"last_attempted_at,omitempty"`
	ReasonCode      string     `json:"reason_code,omitempty"`
	Message         string     `json:"message,omitempty"`
}

// UserFlowState 表示用户首次跑通招聘任务的流程快照。
type UserFlowState struct {
	Version        int                          `json:"version"`
	Stage          string                       `json:"stage"`
	StageName      string                       `json:"stage_name"`
	State          string                       `json:"state"`
	ReasonCode     string                       `json:"reason_code,omitempty"`
	Message        string                       `json:"message,omitempty"`
	LastActivityAt *time.Time                   `json:"last_activity_at,omitempty"`
	CompletedAt    *time.Time                   `json:"completed_at,omitempty"`
	Steps          map[string]UserFlowStepState `json:"steps"`
}

// UserFlowUpdate 表示一次流程节点变更。
type UserFlowUpdate struct {
	Step       string         `json:"step"`
	Status     string         `json:"status"`
	ReasonCode string         `json:"reason_code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Source     string         `json:"source,omitempty"`
	TaskID     string         `json:"task_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	OccurredAt time.Time      `json:"-"`
}

// UserFlowStore 定义用户流程快照和事件存储能力。
type UserFlowStore interface {
	Get(email string) (UserFlowState, error)
	Record(email string, update UserFlowUpdate) (UserFlowState, error)
}

// UserFlowService 处理当前登录用户的流程状态读取和上报。
type UserFlowService struct {
	auth  *AuthService
	store UserFlowStore
}

// NewUserFlowService 创建用户流程服务。
func NewUserFlowService(auth *AuthService, store UserFlowStore) *UserFlowService {
	return &UserFlowService{auth: auth, store: store}
}

// Current 读取或上报当前登录用户的流程状态。
func (s *UserFlowService) Current(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	switch r.Method {
	case http.MethodGet:
		state, err := s.store.Get(session.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load user flow")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "flow": state})
	case http.MethodPost:
		var update UserFlowUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		update.Source = defaultString(strings.TrimSpace(update.Source), "frontend")
		if err := validateUserFlowUpdate(update); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		state, err := s.store.Record(session.Email, update)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record user flow")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "flow": state})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// MemoryUserFlowStore 在开发和测试环境中保存用户流程状态。
type MemoryUserFlowStore struct {
	mu    sync.Mutex
	items map[string]UserFlowState
}

// NewMemoryUserFlowStore 创建内存用户流程存储。
func NewMemoryUserFlowStore() *MemoryUserFlowStore {
	return &MemoryUserFlowStore{items: map[string]UserFlowState{}}
}

// Get 读取内存中的用户流程状态。
func (s *MemoryUserFlowStore) Get(email string) (UserFlowState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.items[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return defaultUserFlowState(), nil
	}
	return normalizeUserFlowState(state), nil
}

// Record 写入内存中的用户流程事件并刷新快照。
func (s *MemoryUserFlowStore) Record(email string, update UserFlowUpdate) (UserFlowState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(email))
	state := normalizeUserFlowState(s.items[key])
	next, err := applyUserFlowUpdate(state, update)
	if err != nil {
		return UserFlowState{}, err
	}
	s.items[key] = next
	return next, nil
}

// defaultUserFlowState 创建尚未开始的用户流程快照。
func defaultUserFlowState() UserFlowState {
	state := UserFlowState{Version: userFlowVersion, Steps: map[string]UserFlowStepState{}}
	return deriveUserFlowState(state)
}

// normalizeUserFlowState 补全旧数据或空数据缺少的流程字段。
func normalizeUserFlowState(state UserFlowState) UserFlowState {
	state.Version = userFlowVersion
	if state.Steps == nil {
		state.Steps = map[string]UserFlowStepState{}
	}
	return deriveUserFlowState(state)
}

// applyUserFlowUpdate 将单次节点事件合并到流程快照。
func applyUserFlowUpdate(state UserFlowState, update UserFlowUpdate) (UserFlowState, error) {
	status := strings.TrimSpace(update.Status)
	if status == "" {
		status = "completed"
	}
	update.Status = status
	if err := validateUserFlowUpdate(update); err != nil {
		return UserFlowState{}, err
	}
	state = normalizeUserFlowState(state)
	now := update.OccurredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	index := userFlowStepIndex(update.Step)
	if update.Status == "completed" {
		for position := 0; position < index; position++ {
			key := userFlowStepOrder[position]
			step := state.Steps[key]
			if step.Status != "completed" {
				step.Status = "completed"
				step.CompletedAt = timePointer(now)
				step.ReasonCode = ""
				step.Message = ""
				state.Steps[key] = step
			}
		}
	}
	step := state.Steps[update.Step]
	step.LastAttemptedAt = timePointer(now)
	if update.Status == "completed" {
		step.Status = "completed"
		if step.CompletedAt == nil {
			step.CompletedAt = timePointer(now)
		}
		step.ReasonCode = ""
		step.Message = ""
	} else if step.Status != "completed" {
		step.Status = update.Status
		step.ReasonCode = strings.TrimSpace(update.ReasonCode)
		step.Message = limitUserFlowText(update.Message, 300)
	}
	state.Steps[update.Step] = step
	state.LastActivityAt = timePointer(now)
	return deriveUserFlowState(state), nil
}

// deriveUserFlowState 根据节点状态计算当前阶段和展示信息。
func deriveUserFlowState(state UserFlowState) UserFlowState {
	state.Version = userFlowVersion
	if state.Steps == nil {
		state.Steps = map[string]UserFlowStepState{}
	}
	state.CompletedAt = nil
	for _, key := range userFlowStepOrder {
		step := state.Steps[key]
		if step.Status == "completed" {
			continue
		}
		state.Stage = key
		state.StageName = userFlowStepNames[key]
		state.State = defaultString(step.Status, "pending")
		state.ReasonCode = step.ReasonCode
		state.Message = step.Message
		return state
	}
	state.Stage = "completed"
	state.StageName = "核心流程已跑通"
	state.State = "completed"
	state.ReasonCode = ""
	state.Message = ""
	last := state.Steps[userFlowFirstGreetSuccess].CompletedAt
	state.CompletedAt = last
	return state
}

// validateUserFlowUpdate 校验流程节点上报参数。
func validateUserFlowUpdate(update UserFlowUpdate) error {
	if userFlowStepIndex(update.Step) < 0 {
		return errors.New("unsupported user flow step")
	}
	status := strings.TrimSpace(update.Status)
	if status == "" {
		status = "completed"
	}
	if status != "completed" && status != "blocked" && status != "pending" {
		return errors.New("unsupported user flow status")
	}
	return nil
}

// userFlowStepIndex 返回流程节点在主流程中的顺序。
func userFlowStepIndex(step string) int {
	step = strings.TrimSpace(step)
	for index, key := range userFlowStepOrder {
		if key == step {
			return index
		}
	}
	return -1
}

// timePointer 返回指定时间的独立指针。
func timePointer(value time.Time) *time.Time {
	copyValue := value
	return &copyValue
}

// limitUserFlowText 限制流程错误文案长度，避免把大段日志写入用户快照。
func limitUserFlowText(value string, limit int) string {
	items := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(items) <= limit {
		return string(items)
	}
	return string(items[:limit])
}
