// Package httpapi 本文件负责自动回复公司档案、岗位配置、权限状态和公共请求上下文接口。
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const autoReplyJSONBodyLimit = 2 << 20

// AutoReplyService 处理自动回复前端配置和本地 Agent 数据接口。
type AutoReplyService struct {
	auth          *AuthService
	store         *PostgresAutoReplyStore
	tenants       TenantStore
	positions     PositionStore
	accounts      PlatformAccountStore
	candidates    CandidateStore
	subscriptions SubscriptionStore
	systemConfigs SystemConfigStore
	agents        AgentStore
	mailer        Mailer
	aiConfigs     AIConfigStore
	httpClient    *http.Client
	resumeDir     string
}

// autoReplyRequestContext 表示自动回复请求已经验证过的用户、团队和会员上下文。
type autoReplyRequestContext struct {
	Session Session
	Tenant  Tenant
	IsAdmin bool
	Access  SubscriptionAccess
}

// NewAutoReplyService 创建自动回复云端服务并注入存储、会员、设备和邮件能力。
func NewAutoReplyService(auth *AuthService, store *PostgresAutoReplyStore, tenants TenantStore, positions PositionStore, accounts PlatformAccountStore, candidates CandidateStore, subscriptions SubscriptionStore, systemConfigs SystemConfigStore, agents AgentStore, mailer Mailer, aiConfigs AIConfigStore, resumeDir string) *AutoReplyService {
	return &AutoReplyService{
		auth: auth, store: store, tenants: tenants, positions: positions, accounts: accounts,
		candidates: candidates, subscriptions: subscriptions, systemConfigs: systemConfigs,
		agents: agents, mailer: mailer, aiConfigs: aiConfigs,
		httpClient: newAIConfigTestHTTPClient(), resumeDir: strings.TrimSpace(resumeDir),
	}
}

// CompanyProfiles 读取或新增当前团队的公司档案。
func (s *AutoReplyService) CompanyProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listCompanyProfiles(w, r)
	case http.MethodPost:
		s.saveCompanyProfile(w, r, "")
	default:
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个接口暂时不支持这种操作")
	}
}

// CompanyProfile 更新或删除当前团队指定公司档案。
func (s *AutoReplyService) CompanyProfile(w http.ResponseWriter, r *http.Request) {
	profileID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/company-profiles/"), "/")
	if profileID == "" {
		writeAutoReplyError(w, http.StatusBadRequest, "COMPANY_PROFILE_REQUIRED", "我还没认出要处理哪份公司档案")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.saveCompanyProfile(w, r, profileID)
	case http.MethodDelete:
		s.deleteCompanyProfile(w, r, profileID)
	default:
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个接口暂时不支持这种操作")
	}
}

// Position 读取或保存指定岗位的自动回复配置和权限状态。
func (s *AutoReplyService) Position(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/positions/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		writeAutoReplyError(w, http.StatusNotFound, "AUTO_REPLY_ROUTE_NOT_FOUND", "这个自动回复地址没认出来")
		return
	}
	switch parts[1] {
	case "config":
		if r.Method == http.MethodGet {
			s.getPositionConfig(w, r, parts[0])
			return
		}
		if r.Method == http.MethodPut {
			s.savePositionConfig(w, r, parts[0])
			return
		}
	case "status":
		if r.Method == http.MethodGet {
			s.getPositionStatus(w, r, parts[0])
			return
		}
	}
	writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个自动回复地址暂时不支持这种操作")
}

// listCompanyProfiles 返回当前团队全部公司档案。
func (s *AutoReplyService) listCompanyProfiles(w http.ResponseWriter, r *http.Request) {
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	items, err := s.store.ListCompanyProfiles(r.Context(), requestContext.Tenant.ID)
	if err != nil {
		writeAutoReplyInternalError(w, "COMPANY_PROFILE_LIST_FAILED", "公司档案暂时没读出来，请稍后再试", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "company_profiles": items})
}

// saveCompanyProfile 新增或更新一份团队共享公司档案。
func (s *AutoReplyService) saveCompanyProfile(w http.ResponseWriter, r *http.Request, profileID string) {
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	var item CompanyProfile
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.ID = profileID
	item.TenantID = requestContext.Tenant.ID
	saved, err := s.store.SaveCompanyProfile(r.Context(), requestContext.Tenant.ID, requestContext.Session.Email, item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "公司档案没保存成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "company_profile": saved})
}

// deleteCompanyProfile 仅允许团队管理员删除未被岗位引用的公司档案。
func (s *AutoReplyService) deleteCompanyProfile(w http.ResponseWriter, r *http.Request, profileID string) {
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	if !requestContext.IsAdmin {
		writeAutoReplyError(w, http.StatusForbidden, "TEAM_ADMIN_REQUIRED", "删除公司档案需要团队管理员来操作")
		return
	}
	if err := s.store.DeleteCompanyProfile(r.Context(), requestContext.Tenant.ID, profileID); err != nil {
		writeAutoReplyStoreError(w, err, "公司档案没删除成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// getPositionConfig 返回岗位自动回复配置，尚未配置时返回安全默认值。
func (s *AutoReplyService) getPositionConfig(w http.ResponseWriter, r *http.Request, positionID string) {
	requestContext, position, ok := s.positionRequestContext(w, r, positionID, false, false)
	if !ok {
		return
	}
	config, err := s.store.GetPositionAutoReplyConfig(r.Context(), requestContext.Tenant.ID, position.ID)
	if errors.Is(err, ErrNotFound) {
		config = PositionAutoReplyConfig{PositionID: position.ID, TenantID: requestContext.Tenant.ID}
		applyAutoReplyConfigDefaults(&config)
	} else if err != nil {
		writeAutoReplyInternalError(w, "AUTO_REPLY_CONFIG_LOAD_FAILED", "岗位自动回复配置暂时没读出来", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "position": publicAutoReplyPosition(position), "config": config,
		"subscription": publicSubscriptionAccess(requestContext.Access),
	})
}

// savePositionConfig 原子保存岗位配置，并在开启时统一检查自动回复会员权限。
func (s *AutoReplyService) savePositionConfig(w http.ResponseWriter, r *http.Request, positionID string) {
	requestContext, position, ok := s.positionRequestContext(w, r, positionID, false, false)
	if !ok {
		return
	}
	var item PositionAutoReplyConfig
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.PositionID = position.ID
	item.TenantID = requestContext.Tenant.ID
	if item.Enabled && !requestContext.Access.AllowAutoReply {
		writeAutoReplyError(w, http.StatusForbidden, "AUTO_REPLY_MAX_REQUIRED", "自动回复属于 Max 全能版，当前套餐暂时不能开启")
		return
	}
	saved, err := s.store.SavePositionAutoReplyConfig(r.Context(), requestContext.Session.Email, item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "岗位自动回复配置没保存成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": saved})
}

// getPositionStatus 返回岗位自动回复实时开关、配置版本和会员权限。
func (s *AutoReplyService) getPositionStatus(w http.ResponseWriter, r *http.Request, positionID string) {
	requestContext, position, ok := s.positionRequestContext(w, r, positionID, false, false)
	if !ok {
		return
	}
	config, err := s.store.GetPositionAutoReplyConfig(r.Context(), requestContext.Tenant.ID, position.ID)
	if errors.Is(err, ErrNotFound) {
		config = PositionAutoReplyConfig{PositionID: position.ID, TenantID: requestContext.Tenant.ID}
		applyAutoReplyConfigDefaults(&config)
	} else if err != nil {
		writeAutoReplyInternalError(w, "AUTO_REPLY_STATUS_LOAD_FAILED", "自动回复状态暂时没读出来", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "enabled": config.Enabled && requestContext.Access.AllowAutoReply,
		"configured_enabled": config.Enabled, "version": config.Version,
		"subscription": publicSubscriptionAccess(requestContext.Access),
	})
}

// currentRequestContext 统一校验登录、团队、会员和可选的本地设备绑定。
func (s *AutoReplyService) currentRequestContext(w http.ResponseWriter, r *http.Request, requireAutoReply bool, requireAgent bool) (autoReplyRequestContext, bool) {
	if s.store == nil {
		writeAutoReplyError(w, http.StatusServiceUnavailable, "AUTO_REPLY_STORAGE_UNAVAILABLE", "自动回复存储还没准备好，请稍后再试")
		return autoReplyRequestContext{}, false
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeAutoReplyError(w, http.StatusUnauthorized, "SESSION_EXPIRED", "登录状态已经失效，请重新登录")
		return autoReplyRequestContext{}, false
	}
	tenant, err := s.tenants.GetOrCreateTenant(session.Email)
	if err != nil {
		writeAutoReplyInternalError(w, "TENANT_LOAD_FAILED", "团队信息暂时没读出来", err)
		return autoReplyRequestContext{}, false
	}
	isAdmin, err := s.tenants.IsTenantAdmin(tenant.ID, session.Email)
	if err != nil {
		writeAutoReplyInternalError(w, "TEAM_ROLE_LOAD_FAILED", "团队权限暂时没读出来", err)
		return autoReplyRequestContext{}, false
	}
	subscription, err := s.subscriptions.UserSubscription(session.Email)
	if err != nil {
		writeAutoReplyInternalError(w, "SUBSCRIPTION_CHECK_FAILED", "会员状态暂时没查清楚，请稍后再试", err)
		return autoReplyRequestContext{}, false
	}
	access, err := subscriptionAccess(s.systemConfigs, subscription, time.Now())
	if err != nil {
		writeAutoReplyInternalError(w, "SUBSCRIPTION_CHECK_FAILED", "会员套餐配置暂时没读明白，请稍后再试", err)
		return autoReplyRequestContext{}, false
	}
	if requireAutoReply && !access.AllowAutoReply {
		writeAutoReplyError(w, http.StatusForbidden, "AUTO_REPLY_MAX_REQUIRED", "自动回复属于 Max 全能版，当前套餐暂时不能使用")
		return autoReplyRequestContext{}, false
	}
	if requireAgent {
		machineID := strings.TrimSpace(r.Header.Get("X-GoodHR-Machine-ID"))
		if s.agents == nil {
			writeAutoReplyError(w, http.StatusServiceUnavailable, "DEVICE_BINDING_UNAVAILABLE", "本地程序绑定状态暂时查不了，请稍后再试")
			return autoReplyRequestContext{}, false
		}
		active, agentErr := s.agents.HasActiveBinding(session.Email, machineID)
		if agentErr != nil {
			writeAutoReplyInternalError(w, "DEVICE_BINDING_CHECK_FAILED", "本地程序绑定状态暂时查不了，请稍后再试", agentErr)
			return autoReplyRequestContext{}, false
		}
		if !isStableAgentMachineID(machineID) || !active {
			writeAutoReplyError(w, http.StatusForbidden, "DEVICE_BINDING_REQUIRED", "这台电脑还没有完成账号绑定，请刷新后台后再试")
			return autoReplyRequestContext{}, false
		}
	}
	return autoReplyRequestContext{Session: session, Tenant: tenant, IsAdmin: isAdmin, Access: access}, true
}

// positionRequestContext 校验当前账号拥有目标岗位，并可选检查自动回复和设备权限。
func (s *AutoReplyService) positionRequestContext(w http.ResponseWriter, r *http.Request, positionID string, requireAutoReply bool, requireAgent bool) (autoReplyRequestContext, Position, bool) {
	requestContext, ok := s.currentRequestContext(w, r, requireAutoReply, requireAgent)
	if !ok {
		return autoReplyRequestContext{}, Position{}, false
	}
	position, err := s.positions.PositionByID(requestContext.Tenant.ID, requestContext.Session.Email, strings.TrimSpace(positionID), false)
	if errors.Is(err, ErrNotFound) {
		writeAutoReplyError(w, http.StatusNotFound, "POSITION_NOT_FOUND", "这个岗位没有找到，可能已经被删除了")
		return autoReplyRequestContext{}, Position{}, false
	}
	if err != nil {
		writeAutoReplyInternalError(w, "POSITION_LOAD_FAILED", "岗位信息暂时没读出来", err)
		return autoReplyRequestContext{}, Position{}, false
	}
	return requestContext, position, true
}

// decodeAutoReplyJSON 按大小限制读取强类型 JSON 请求，并拒绝未知字段。
func decodeAutoReplyJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, autoReplyJSONBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "INVALID_REQUEST", "这次参数没读明白，请检查后再试")
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAutoReplyError(w, http.StatusBadRequest, "INVALID_REQUEST", "一次请求只能提交一份数据")
		return fmt.Errorf("request contains trailing data")
	}
	return nil
}

// writeAutoReplyStoreError 把存储边界错误转换成短而明确的用户提示。
func writeAutoReplyStoreError(w http.ResponseWriter, err error, fallback string) {
	var validationError *autoReplyValidationError
	switch {
	case errors.Is(err, ErrNotFound):
		writeAutoReplyError(w, http.StatusNotFound, "AUTO_REPLY_DATA_NOT_FOUND", "相关数据没找到，可能刚刚被删除了")
	case errors.Is(err, ErrAutoReplyForbidden):
		writeAutoReplyError(w, http.StatusForbidden, "AUTO_REPLY_FORBIDDEN", "这份数据不属于当前团队，我先不敢动")
	case errors.As(err, &validationError):
		writeAutoReplyError(w, http.StatusBadRequest, "AUTO_REPLY_VALIDATION_FAILED", validationError.Error())
	default:
		writeAutoReplyInternalError(w, "AUTO_REPLY_STORAGE_FAILED", fallback, err)
	}
}

// writeAutoReplyError 返回包含稳定错误码的自动回复错误。
func writeAutoReplyError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": map[string]string{"code": code, "message": message}})
}

// writeAutoReplyInternalError 记录完整服务端错误，并只把安全提示和错误编号返回给调用方。
func writeAutoReplyInternalError(w http.ResponseWriter, code string, message string, err error) {
	errorID := newAutoReplyErrorID()
	log.Printf("[自动回复] 错误编号=%s code=%s err=%v", errorID, code, err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"ok":    false,
		"error": map[string]string{"code": code, "message": message, "error_id": errorID},
	})
}

// newAutoReplyErrorID 生成便于用户和服务端日志互相定位的短错误编号。
func newAutoReplyErrorID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

// publicAutoReplyPosition 返回自动回复配置需要的最小岗位信息。
func publicAutoReplyPosition(position Position) map[string]any {
	return map[string]any{"id": position.ID, "name": position.Name, "platform_id": position.PlatformID, "status": position.Status}
}
