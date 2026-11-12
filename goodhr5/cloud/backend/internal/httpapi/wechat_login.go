// 本文件提供公众号验证码免密注册登录：页面持有独占凭据，公众号回调确认身份，只有原页面同意协议后才能换取登录态。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strings"
)

var wechatLoginCommand = regexp.MustCompile(`^验证码[\s：:]*([0-9]{4}|[0-9]{8})$`)

// wechatLoginPollRequest 只从生成登录码的页面接受随机凭据；公开短码不能用来领取登录态。
type wechatLoginPollRequest struct {
	Ticket            string `json:"ticket"`
	AgreementAccepted bool   `json:"agreement_accepted"`
	InviterID         string `json:"inviter_id"`
}

// wechatLoginConfig 检查公众号已启用且回调参数可用，配置故障时保留原邮箱入口。
func (s *WechatOfficialService) wechatLoginConfig() (WechatOfficialConfig, error) {
	config, err := loadWechatOfficialConfig(s.configs)
	if err != nil || config.LoginEnabled == nil || !*config.LoginEnabled || !config.configured() {
		return WechatOfficialConfig{}, errWechatLoginUnavailable
	}
	if _, ok := s.store.(WechatLoginAccountStore); !ok {
		return WechatOfficialConfig{}, errWechatLoginUnavailable
	}
	return config, nil
}

// CreateLogin 为当前页面生成五分钟有效的四位短码与256位随机凭据，尚未确认微信或协议时不创建账号。
func (s *WechatOfficialService) CreateLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "请用 POST 获取登录码")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	config, err := s.wechatLoginConfig()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var request struct {
		PreviousTicket string `json:"previous_ticket"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "登录页参数没读完整，请重试")
		return
	}
	if request.PreviousTicket != "" {
		if !validWechatLoginTicket(request.PreviousTicket) {
			writeError(w, http.StatusBadRequest, "旧登录码凭据不完整，请刷新页面")
			return
		}
	}
	allowed, err := s.loginCache.TakeGeneration(r.Context(), wechatLoginClientIP(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "登录码暂时没准备好，稍后再试")
		return
	}
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "点得有点快，当前登录码仍能用，请稍后再换")
		return
	}

	ticket, err := newWechatMessageNonce()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "登录码生成没成功，请重试")
		return
	}
	previousHash := ""
	if request.PreviousTicket != "" {
		previousHash = wechatBindingCodeHash(request.PreviousTicket)
	}
	for attempt := 0; attempt < wechatBindingGenerationRetries; attempt++ {
		value, err := rand.Int(rand.Reader, big.NewInt(10000))
		if err != nil {
			break
		}
		code := fmt.Sprintf("%04d", value.Int64())
		now := s.now().UTC()
		challenge := wechatLoginChallenge{TicketHash: wechatBindingCodeHash(ticket), CodeHash: wechatBindingCodeHash(code), AppID: config.AppID, CreatedAt: now.Unix(), ExpiresAt: now.Add(wechatLoginTTL).Unix()}
		// 登录与绑定共用短码占位，纯数字消息只能对应其中一种用途。
		reserved, reserveErr := s.cache.ReserveCode(r.Context(), challenge.CodeHash, challenge.TicketHash, wechatLoginTTL)
		if reserveErr != nil {
			break
		}
		if !reserved {
			continue
		}
		created, err := s.loginCache.Create(r.Context(), challenge, previousHash)
		if err != nil {
			break
		}
		if !created {
			if err := s.cache.ReleaseCode(r.Context(), challenge.CodeHash, challenge.TicketHash); err != nil {
				break
			}
			continue
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ticket": ticket, "login_code": code, "qr_image_url": config.QRImageURL, "expires_at": now.Add(wechatLoginTTL).Format("2006-01-02T15:04:05Z07:00")})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "登录码暂时没准备好，可以重新生成")
}

// PollLogin 串行检查当前页面的微信确认，协议未同意时只返回状态；确认后一次消费并复用正式登录流程。
func (s *WechatOfficialService) PollLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "请用 POST 检查登录状态")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	config, err := s.wechatLoginConfig()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var req wechatLoginPollRequest
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || !validWechatLoginTicket(req.Ticket) {
		writeError(w, http.StatusBadRequest, "这个登录页面的凭据不完整，请重新生成登录码")
		return
	}
	challenge, err := s.loginCache.Read(r.Context(), wechatBindingCodeHash(req.Ticket), false)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "expired"})
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "暂时没查到登录结果，请稍后重试")
		return
	}
	if challenge.AppID != config.AppID {
		writeError(w, http.StatusConflict, "公众号设置已变化，请重新生成登录码")
		return
	}
	if challenge.OpenID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "waiting"})
		return
	}
	agreementAccepted := req.AgreementAccepted
	if !agreementAccepted {
		email, lookupErr := s.store.(WechatLoginAccountStore).FindWechatLogin(r.Context(), challenge.AppID, challenge.OpenID)
		if lookupErr != nil && !errors.Is(lookupErr, ErrNotFound) {
			writeError(w, http.StatusServiceUnavailable, "账号确认有点慢，请稍后重试")
			return
		}
		if lookupErr == nil && s.auth.userActivity != nil {
			agreementAccepted, err = s.auth.userActivity.HasAcceptedAgreement(email)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "协议状态暂时没查到，请稍后重试")
				return
			}
		}
	}
	if !agreementAccepted {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "agreement_required"})
		return
	}
	challenge, err = s.loginCache.Read(r.Context(), wechatBindingCodeHash(req.Ticket), true)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "expired"})
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "登录确认没完成，请重新生成登录码")
		return
	}
	email, err := s.store.(WechatLoginAccountStore).ResolveWechatLogin(r.Context(), challenge.AppID, challenge.OpenID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "微信已确认，但账号暂时没准备好，请重新生成登录码再试")
		return
	}
	s.auth.completeLogin(w, email, strings.TrimSpace(req.InviterID), true, false, "wechat")
}

// handleLoginMessage 只处理已完成微信签名校验的“验证码 四位数字”消息，未知或过期码不能创建账号或泄漏页面凭据。
func (s *WechatOfficialService) handleLoginMessage(ctx context.Context, config WechatOfficialConfig, message wechatIncomingMessage) (string, error) {
	parts := wechatLoginCommand.FindStringSubmatch(strings.TrimSpace(message.Content))
	if len(parts) != 2 {
		log.Printf("GoodHR wechat login_message status=invalid_format")
		return "请把 GoodHR 登录页上的“验证码 四位数字”完整发给我。", nil
	}
	appID, openID, err := normalizeWechatIdentity(config.AppID, message.FromUserName)
	if err != nil {
		return "", err
	}
	ticket, err := s.cache.ReserveAttempt(ctx, appID, openID)
	if err != nil {
		return "", err
	}
	if ticket == "" {
		return "验证码刚刚试得有点多，请稍后再试。", nil
	}
	approved, err := s.loginCache.Approve(ctx, wechatBindingCodeHash(parts[1]), appID, openID, message.CreateTime)
	if finishErr := s.cache.FinishAttempt(ctx, appID, openID, ticket, err == nil && !approved); finishErr != nil {
		return "", finishErr
	}
	if err != nil {
		return "", err
	}
	if !approved {
		log.Printf("GoodHR wechat login_message status=expired_or_unmatched")
		return "这个登录码已过期或没对上，请回 GoodHR 重新生成。", nil
	}
	log.Printf("GoodHR wechat login_message status=approved")
	return "微信确认好了，请回刚才的 GoodHR 登录页，页面会自动进入。只发送你自己登录页上的验证码。", nil
}

// wechatLoginClientIP 仅接受本机或内网反向代理写入的单个真实IP，公网直连不能伪造限流来源。
func wechatLoginClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer != nil && (peer.IsLoopback() || peer.IsPrivate()) {
		if address := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); address != nil {
			return address.String()
		}
	}
	return host
}
