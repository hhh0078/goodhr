// 本文件负责插件首次配对与已授权安装的登录恢复；原网页凭证仅保存在本地内存，不对插件开放。
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// extensionPair 保存五分钟有效的单次授权请求。
type extensionPair struct {
	InstallationID string
	Challenge      string
	ExpiresAt      time.Time
	Approved       *cloud.ExtensionSession
	PairingToken   string
	Generation     uint64
}

// extensionPairRequest 定义插件和控制台使用的显式授权参数。
type extensionPairRequest struct {
	InstallationID string `json:"installation_id"`
	Challenge      string `json:"challenge"`
	PairingID      string `json:"pairing_id"`
	Token          string `json:"token"`
	PairingToken   string `json:"pairing_token"`
}

// extensionPairIdentity 只持久化安装授权的摘要和账号 ID，不保存网页凭证。
type extensionPairIdentity struct {
	UserID     string `json:"user_id"`
	SecretHash string `json:"secret_hash"`
}

// handleExtensionPairStart 创建等待控制台用户确认的配对请求，发现服务并不等于授权。
func (s *Server) handleExtensionPairStart(w http.ResponseWriter, r *http.Request) {
	var req extensionPairRequest
	if decodeJSON(w, r, &req) != nil || len(req.InstallationID) < 16 || len(req.InstallationID) > 128 || len(req.Challenge) < 32 || len(req.Challenge) > 128 {
		writeError(w, 400, "INVALID_PAIR_REQUEST", fmt.Errorf("插件配对信息不完整，请重新打开插件"))
		return
	}
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	for id, pair := range s.extensionPairs {
		if time.Now().After(pair.ExpiresAt) {
			delete(s.extensionPairs, id)
		}
	}
	if len(s.extensionPairs) >= 8 {
		writeError(w, 429, "PAIR_BUSY", fmt.Errorf("配对请求有点多，请稍后再试"))
		return
	}
	id, err := extensionRandomToken()
	if err != nil {
		writeError(w, 500, "PAIR_UNAVAILABLE", err)
		return
	}
	s.extensionPairs[id] = &extensionPair{InstallationID: req.InstallationID, Challenge: req.Challenge, ExpiresAt: time.Now().Add(5 * time.Minute)}
	consoleURL := "https://goodhr.58it.cn/admin/resumes"
	if base, err := url.Parse(s.cfg.CloudURL); err == nil && (base.Hostname() == "127.0.0.1" || base.Hostname() == "localhost") {
		consoleURL = "http://127.0.0.1:5173/admin/resumes"
	}
	approveURL := consoleURL + "?extension_pair=" + url.QueryEscape(id) + fmt.Sprintf("&agent_port=%d", s.cfg.Port)
	writeSuccess(w, 200, struct {
		PairingID  string `json:"pairing_id"`
		ExpiresIn  int    `json:"expires_in"`
		ApproveURL string `json:"approve_url"`
	}{id, 300, approveURL})
}

// handleExtensionPairApprove 使用控制台显式提交的会话验证账号，并签发受限插件凭证。
func (s *Server) handleExtensionPairApprove(w http.ResponseWriter, r *http.Request) {
	if !allowedOrigin(r.Header.Get("Origin")) {
		writeError(w, 403, "PAIR_ORIGIN_DENIED", fmt.Errorf("请从 GoodHR 控制台确认配对"))
		return
	}
	var req extensionPairRequest
	if decodeJSON(w, r, &req) != nil || req.Token == "" || s.cloud == nil {
		writeError(w, 400, "PAIR_TOKEN_REQUIRED", fmt.Errorf("请先登录 GoodHR 控制台"))
		return
	}
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	pair := s.extensionPairs[req.PairingID]
	if pair == nil || time.Now().After(pair.ExpiresAt) || pair.Approved != nil {
		writeError(w, 409, "PAIR_EXPIRED", fmt.Errorf("这个配对请求已经过期或处理过了，请重新发起"))
		return
	}
	user, err := s.cloud.ValidateSession(r.Context(), req.Token)
	if err != nil {
		writeError(w, 401, "SESSION_INVALID", err)
		return
	}
	session, err := s.cloud.ExchangeExtensionSession(r.Context(), req.Token)
	if err != nil || session.Token == "" {
		writeError(w, 502, "PAIR_EXCHANGE_FAILED", fmt.Errorf("插件授权暂时没完成，请再试一次"))
		return
	}
	secret, err := extensionRandomToken()
	if err != nil {
		writeError(w, 500, "PAIR_UNAVAILABLE", err)
		return
	}
	identity := extensionPairIdentity{UserID: user.UserID, SecretHash: extensionHash(secret)}
	if s.store == nil {
		writeError(w, 503, "PAIR_STORAGE_UNAVAILABLE", fmt.Errorf("本地存储还没准备好，请重启后再试"))
		return
	}
	value, _ := json.Marshal(identity)
	if err = s.store.SetSetting(r.Context(), extensionIdentityKey(pair.InstallationID), string(value)); err != nil {
		writeError(w, 500, "PAIR_SAVE_FAILED", err)
		return
	}
	s.bindExtensionSessionLocked(req.Token, user.UserID)
	pair.Approved, pair.PairingToken, pair.Generation = &session, secret, s.extensionGeneration
	s.extensionPairs[req.PairingID] = pair
	writeSuccess(w, 200, struct {
		Approved bool   `json:"approved"`
		Email    string `json:"email"`
	}{true, session.User.Email})
}

// handleExtensionPairStatus 单次交付已经确认的受限会话；只有持有安装 ID 和随机挑战的插件可取回。
func (s *Server) handleExtensionPairStatus(w http.ResponseWriter, r *http.Request) {
	var req extensionPairRequest
	if decodeJSON(w, r, &req) != nil {
		writeError(w, 400, "INVALID_PAIR_REQUEST", fmt.Errorf("配对状态信息不完整"))
		return
	}
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	pair := s.extensionPairs[req.PairingID]
	if pair == nil || time.Now().After(pair.ExpiresAt) || pair.InstallationID != req.InstallationID || !extensionSecretsEqual(pair.Challenge, req.Challenge) {
		writeError(w, 404, "PAIR_NOT_FOUND", fmt.Errorf("配对请求已过期，请重新连接"))
		return
	}
	if pair.Approved == nil {
		writeSuccess(w, 200, struct {
			Status string `json:"status"`
		}{"pending"})
		return
	}
	if pair.Generation != s.extensionGeneration || pair.Approved.User.ID != s.extensionUserID {
		delete(s.extensionPairs, req.PairingID)
		writeError(w, 401, "PAIR_ACCOUNT_CHANGED", fmt.Errorf("本地账号已变化，请重新连接"))
		return
	}
	result := struct {
		cloud.ExtensionSession
		Status       string `json:"status"`
		PairingToken string `json:"pairing_token"`
	}{*pair.Approved, "approved", pair.PairingToken}
	delete(s.extensionPairs, req.PairingID)
	writeSuccess(w, 200, result)
}

// handleExtensionRestore 仅为曾经确认且账号一致的插件恢复登录，账户变化时要求重新配对。
func (s *Server) handleExtensionRestore(w http.ResponseWriter, r *http.Request) {
	var req extensionPairRequest
	if decodeJSON(w, r, &req) != nil || len(req.InstallationID) < 16 || len(req.PairingToken) < 32 || s.store == nil || s.cloud == nil {
		writeError(w, 401, "PAIR_REQUIRED", fmt.Errorf("请先连接本地程序，或手动登录插件"))
		return
	}
	s.extensionMu.Lock()
	token, userID, generation := s.extensionToken, s.extensionUserID, s.extensionGeneration
	s.extensionMu.Unlock()
	value, err := s.store.GetSetting(r.Context(), extensionIdentityKey(req.InstallationID))
	var identity extensionPairIdentity
	if err != nil || json.Unmarshal([]byte(value), &identity) != nil || identity.UserID == "" || identity.UserID != userID || !extensionSecretsEqual(identity.SecretHash, extensionHash(req.PairingToken)) || token == "" {
		writeError(w, 401, "PAIR_REQUIRED", fmt.Errorf("本地账号尚未登录或已切换，请重新配对"))
		return
	}
	user, err := s.cloud.ValidateSession(r.Context(), token)
	if err != nil || user.UserID != identity.UserID {
		writeError(w, 401, "SESSION_INVALID", fmt.Errorf("本地登录已过期，请先在控制台重新登录"))
		return
	}
	session, err := s.cloud.ExchangeExtensionSession(r.Context(), token)
	if err != nil {
		writeError(w, 502, "PAIR_EXCHANGE_FAILED", err)
		return
	}
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	current, readErr := s.store.GetSetting(r.Context(), extensionIdentityKey(req.InstallationID))
	if generation != s.extensionGeneration || userID != s.extensionUserID || readErr != nil || current != value {
		_ = s.cloud.RevokeExtensionSession(r.Context(), session.Token)
		writeError(w, 401, "PAIR_ACCOUNT_CHANGED", fmt.Errorf("本地账号或连接授权已变化，请重新连接"))
		return
	}
	writeSuccess(w, 200, session)
}

// handleExtensionPairRevoke 验证安装密钥后撤销本地恢复授权，重复撤销保持成功。
func (s *Server) handleExtensionPairRevoke(w http.ResponseWriter, r *http.Request) {
	var req extensionPairRequest
	if decodeJSON(w, r, &req) != nil || len(req.InstallationID) < 16 || len(req.InstallationID) > 128 || len(req.PairingToken) < 32 || len(req.PairingToken) > 128 || s.store == nil {
		writeError(w, 400, "PAIR_REVOKE_INVALID", fmt.Errorf("连接授权信息不完整，请重试"))
		return
	}
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	key := extensionIdentityKey(req.InstallationID)
	value, err := s.store.GetSetting(r.Context(), key)
	var identity extensionPairIdentity
	if err != nil {
		writeError(w, 500, "PAIR_STORAGE_UNAVAILABLE", err)
		return
	}
	if value != "" && (json.Unmarshal([]byte(value), &identity) != nil || !extensionSecretsEqual(identity.SecretHash, extensionHash(req.PairingToken))) {
		writeError(w, 401, "PAIR_REVOKE_DENIED", fmt.Errorf("连接授权不匹配，请重新打开插件"))
		return
	}
	if err = s.store.SetSetting(r.Context(), key, ""); err != nil {
		writeError(w, 500, "PAIR_REVOKE_FAILED", err)
		return
	}
	s.resumeSummaries.CancelActive("extension:" + extensionHash(req.InstallationID))
	for id, pair := range s.extensionPairs {
		if pair.InstallationID == req.InstallationID {
			delete(s.extensionPairs, id)
		}
	}
	writeSuccess(w, 200, struct {
		Revoked bool `json:"revoked"`
	}{true})
}

// bindExtensionSessionLocked 更新内存网页登录状态；切号使未领取的配对立即失效，调用方须持有锁。
func (s *Server) bindExtensionSessionLocked(token, userID string) {
	if s.extensionUserID != userID {
		s.extensionPairs = make(map[string]*extensionPair)
	}
	if s.extensionToken != token || s.extensionUserID != userID {
		s.extensionGeneration++
		// 完整登录会话已被替换，旧凭证发起的收费调用只能停止，不能把结果交给新会话。
		if s.extensionToken != "" {
			s.resumeSummaries.CancelActive("")
			s.resumeImports.CancelActive()
		}
	}
	s.extensionToken, s.extensionUserID = token, userID
	if s.runner != nil {
		s.runner.BindScreeningSession(userID, token)
	}
	if s.resumeDownloads != nil {
		s.resumeDownloads.Bind(userID, token)
	}
}

// extensionRandomToken 生成不依赖时间或账号的随机授权值。
func extensionRandomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

// extensionHash 计算配对密钥摘要，用于本地持久化与安装 ID 命名。
func extensionHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// extensionIdentityKey 返回不包含用户凭证的设置键。
func extensionIdentityKey(id string) string {
	return "extension_pair:" + extensionHash(strings.TrimSpace(id))
}

// extensionSecretsEqual 使用固定时间比较授权值，拒绝空值。
func extensionSecretsEqual(left, right string) bool {
	return left != "" && right != "" && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
