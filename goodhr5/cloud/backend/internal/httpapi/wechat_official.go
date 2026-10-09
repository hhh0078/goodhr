// 本文件负责公众号回调、登录用户绑定码和一次性关注奖励；招聘执行流程不依赖公众号。
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const wechatBindingTTL = 10 * time.Minute
const wechatCallbackLimit = 1 << 20
const wechatBindingGenerationRetries = 64

var wechatAcceptedBindingCode = regexp.MustCompile(`^(?:[1-9][0-9]{3}|[23456789A-HJ-NP-Z]{8})$`)

// WechatOfficialService 验证微信身份并把公众号绑定到当前登录账号。
type WechatOfficialService struct {
	auth    *AuthService
	store   WechatFollowStore
	configs SystemConfigStore
	now     func() time.Time
	cache   wechatBindingCache
	newCode func() (string, error)
}

// wechatIncomingMessage 对应微信明文或加密回调的 XML 字段。
type wechatIncomingMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Event        string   `xml:"Event"`
	Content      string   `xml:"Content"`
	Encrypt      string   `xml:"Encrypt"`
}

// wechatTextReply 对应微信被动文本回复，XML 编码负责安全转义内容。
type wechatTextReply struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
}

// wechatEncryptedReply 对应微信安全模式要求的加密回复封套。
type wechatEncryptedReply struct {
	XMLName      xml.Name `xml:"xml"`
	Encrypt      string   `xml:"Encrypt"`
	MsgSignature string   `xml:"MsgSignature"`
	TimeStamp    string   `xml:"TimeStamp"`
	Nonce        string   `xml:"Nonce"`
}

// NewWechatOfficialService 创建公众号业务服务，时间函数可由测试替换。
func NewWechatOfficialService(auth *AuthService, store WechatFollowStore, configs SystemConfigStore) *WechatOfficialService {
	service := &WechatOfficialService{auth: auth, store: store, configs: configs, now: time.Now, newCode: newWechatBindingCode}
	service.cache = newMemoryWechatBindingCache(func() time.Time { return service.now() })
	if auth != nil {
		if redisStore, ok := auth.store.(*RedisAuthStore); ok {
			service.cache = &redisWechatBindingCache{client: redisStore.client}
		}
	}
	return service
}

// publicStatus 只返回当前用户的状态及公开二维码信息，不返回 OpenID 或密钥。
func (s *WechatOfficialService) publicStatus(ctx context.Context, email string, config WechatOfficialConfig) (map[string]any, error) {
	status, err := s.store.BindingStatus(ctx, email)
	if err != nil {
		return nil, err
	}
	days, err := loadWechatFollowRewardDays(s.configs)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "enabled": config.Enabled, "configured": config.configured(),
		"account_name": config.AccountName, "original_id": config.OriginalID,
		"qr_image_url": config.QRImageURL, "reward_days": days,
		"bound": status.Bound, "followed": status.Followed, "gift_granted": status.GiftGranted, "gift_days": status.GiftDays,
	}, nil
}

// Status 查询登录用户本人是否已绑定及是否已领取关注奖励。
func (s *WechatOfficialService) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "这个接口请用 GET")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先登录，再查看公众号绑定")
		return
	}
	config, err := loadWechatOfficialConfig(s.configs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "公众号配置暂时没读好，稍后再试")
		return
	}
	status, err := s.publicStatus(r.Context(), session.Email, config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "绑定状态暂时没查到，稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// CreateBinding 为当前登录用户生成十分钟有效的一次性绑定码，旧待绑定码随之失效。
func (s *WechatOfficialService) CreateBinding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "这个接口请用 POST")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先登录，再领取公众号绑定码")
		return
	}
	config, err := loadWechatOfficialConfig(s.configs)
	if err != nil || !config.Enabled || !config.configured() {
		writeError(w, http.StatusServiceUnavailable, "公众号绑定还没准备好，可以先继续工作")
		return
	}
	status, err := s.publicStatus(r.Context(), session.Email, config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "绑定状态暂时没查到，点一下重试")
		return
	}
	allowed, err := s.cache.TakeGeneration(r.Context(), session.Email)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "绑定码暂时没准备好，稍后再试")
		return
	}
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "绑定码刚刚生成得有点多，已有的码仍能用，十分钟后再来就行")
		return
	}
	code, expiresAt, err := s.createAvailableBinding(r.Context(), session.Email)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "绑定码这会儿有点挤，已有的码仍能用，稍后再来试试")
		return
	}
	// 通用前端请求器将顶层code视为业务状态码，绑定码使用独立字段避免误判成功响应。
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "binding_code": code, "expires_at": expiresAt.Format(time.RFC3339), "status": status})
}

// createAvailableBinding 有界重试缓存与数据库碰撞，只有成功创建新码才会作废账号旧码。
func (s *WechatOfficialService) createAvailableBinding(ctx context.Context, email string) (string, time.Time, error) {
	owner, err := newWechatMessageNonce()
	if err != nil {
		return "", time.Time{}, err
	}
	for attempt := 0; attempt < wechatBindingGenerationRetries; attempt++ {
		if err = ctx.Err(); err != nil {
			return "", time.Time{}, err
		}
		code, codeErr := s.newCode()
		if codeErr != nil {
			return "", time.Time{}, codeErr
		}
		hash := wechatBindingCodeHash(code)
		reserved, cacheErr := s.cache.ReserveCode(ctx, hash, owner, wechatBindingTTL)
		if cacheErr != nil {
			return "", time.Time{}, cacheErr
		}
		if !reserved {
			continue
		}
		expiresAt := s.now().UTC().Add(wechatBindingTTL)
		storeErr := s.store.CreateBinding(ctx, email, hash, expiresAt)
		if storeErr == nil {
			return code, expiresAt, nil
		}
		if releaseErr := s.cache.ReleaseCode(ctx, hash, owner); releaseErr != nil {
			return "", time.Time{}, releaseErr
		}
		if !errors.Is(storeErr, ErrWechatBindingCodeConflict) {
			return "", time.Time{}, storeErr
		}
	}
	return "", time.Time{}, ErrWechatBindingCodeConflict
}

// newWechatBindingCode 使用无偏密码学随机整数生成1000至9999的四位数字码。
func newWechatBindingCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(value.Int64()+1000, 10), nil
}

// newWechatMessageNonce 为加密回复与缓存所有者独立生成256位随机值，绝不复用短绑定码。
func newWechatMessageNonce() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

// wechatBindingCodeHash 只向存储层提供绑定码哈希，不保存用户可见的明文码。
func wechatBindingCodeHash(code string) string {
	digest := sha256.Sum256([]byte(code))
	return hex.EncodeToString(digest[:])
}

// Callback 校验微信的 GET 通知地址验证或 POST 消息事件，并支持安全模式。
func (s *WechatOfficialService) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	config, err := loadWechatOfficialConfig(s.configs)
	if err != nil || !config.configured() {
		http.Error(w, "callback not configured", http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query()
	timestamp, nonce := query.Get("timestamp"), query.Get("nonce")
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || nonce == "" || len(nonce) > 256 || len(timestamp) > 16 || unix < s.now().Add(-10*time.Minute).Unix() || unix > s.now().Add(10*time.Minute).Unix() {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		echo := query.Get("echostr")
		if echo == "" || len(echo) > 4096 {
			http.Error(w, "invalid echo", http.StatusBadRequest)
			return
		}
		if query.Get("msg_signature") != "" || query.Get("encrypt_type") == "aes" {
			if !verifyWechatMessageSignature(query.Get("msg_signature"), config.Token, timestamp, nonce, echo) {
				http.Error(w, "invalid signature", http.StatusForbidden)
				return
			}
			plain, err := decryptWechatMessage(echo, config.EncodingAESKey, config.AppID)
			if err != nil {
				http.Error(w, "invalid encrypted echo", http.StatusBadRequest)
				return
			}
			echo = string(plain)
		} else if !verifyWechatMessageSignature(query.Get("signature"), config.Token, timestamp, nonce) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, echo)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, wechatCallbackLimit)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "message too large", http.StatusRequestEntityTooLarge)
		return
	}
	var message wechatIncomingMessage
	if xml.Unmarshal(data, &message) != nil {
		http.Error(w, "invalid xml", http.StatusBadRequest)
		return
	}
	encrypted := message.Encrypt != ""
	if encrypted {
		if !verifyWechatMessageSignature(query.Get("msg_signature"), config.Token, timestamp, nonce, message.Encrypt) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
		data, err = decryptWechatMessage(message.Encrypt, config.EncodingAESKey, config.AppID)
		if err != nil {
			http.Error(w, "invalid encrypted message", http.StatusBadRequest)
			return
		}
		message = wechatIncomingMessage{}
		if xml.Unmarshal(data, &message) != nil {
			http.Error(w, "invalid encrypted xml", http.StatusBadRequest)
			return
		}
	} else {
		// 安全模式标记不得降级为明文，避免密文缺失时绕过消息签名。
		if config.MessageMode == "safe" || config.MessageMode == "" || query.Get("encrypt_type") == "aes" || query.Get("msg_signature") != "" || !verifyWechatMessageSignature(query.Get("signature"), config.Token, timestamp, nonce) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	}
	if message.ToUserName != config.OriginalID || message.FromUserName == "" || len(message.FromUserName) > 128 || message.CreateTime <= 0 || message.CreateTime > s.now().Add(10*time.Minute).Unix() {
		http.Error(w, "invalid message recipient", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	text, err := s.handleMessage(ctx, config, message)
	if err != nil {
		// 非200让微信重试，不能在落库失败时回复绑定成功。
		http.Error(w, "message temporarily unavailable", http.StatusInternalServerError)
		return
	}
	if text == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "success")
		return
	}
	reply, err := xml.Marshal(wechatTextReply{ToUserName: message.FromUserName, FromUserName: config.OriginalID, CreateTime: s.now().Unix(), MsgType: "text", Content: text})
	if err != nil {
		http.Error(w, "reply unavailable", http.StatusInternalServerError)
		return
	}
	if encrypted {
		cipherText, err := encryptWechatMessage(reply, config.EncodingAESKey, config.AppID)
		if err != nil {
			http.Error(w, "reply unavailable", http.StatusInternalServerError)
			return
		}
		replyTimestamp := strconv.FormatInt(s.now().Unix(), 10)
		replyNonce, err := newWechatMessageNonce()
		if err != nil {
			http.Error(w, "reply unavailable", http.StatusInternalServerError)
			return
		}
		reply, err = xml.Marshal(wechatEncryptedReply{Encrypt: cipherText, MsgSignature: wechatMessageSignature(config.Token, replyTimestamp, replyNonce, cipherText), TimeStamp: replyTimestamp, Nonce: replyNonce})
		if err != nil {
			http.Error(w, "reply unavailable", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(reply)
}

// handleMessage 仅根据已验证的微信回调记录关注及消费绑定码，禁止根据二维码参数猜测身份。
func (s *WechatOfficialService) handleMessage(ctx context.Context, config WechatOfficialConfig, message wechatIncomingMessage) (string, error) {
	if message.MsgType == "event" {
		switch strings.ToLower(message.Event) {
		case "subscribe", "unsubscribe":
			followed := strings.EqualFold(message.Event, "subscribe")
			if err := s.store.RecordFollow(ctx, config.AppID, message.FromUserName, followed, time.Unix(message.CreateTime, 0)); err != nil {
				return "", err
			}
			if followed && config.Enabled {
				days, err := loadWechatFollowRewardDays(s.configs)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("感谢关注。回到 GoodHR 记下4位数字绑定码，再发到这里，首次绑定赠送 %d 天会员。已领过的朋友也能重新绑定，后续通知我继续认真送达。", days), nil
			}
		}
		return "", nil
	}
	if message.MsgType != "text" || !config.Enabled {
		return "", nil
	}
	days, err := loadWechatFollowRewardDays(s.configs)
	if err != nil {
		return "", err
	}
	code := strings.ToUpper(strings.TrimSpace(message.Content))
	if !wechatAcceptedBindingCode.MatchString(code) {
		return fmt.Sprintf("绑定 GoodHR：请先登录后台，打开「关注公众号」，记下4位数字绑定码发到这里。首次绑定赠送 %d 天会员，每个账号和微信只领一次。", days), nil
	}
	ticket, err := s.cache.ReserveAttempt(ctx, config.AppID, message.FromUserName)
	if err != nil {
		return "", err
	}
	result, err := s.store.BindCode(ctx, config.AppID, message.FromUserName, wechatBindingCodeHash(code), s.now(), WechatBindOptions{RewardDays: days, MessageTime: time.Unix(message.CreateTime, 0), ReplayOnly: ticket == ""})
	if ticket != "" {
		if finishErr := s.cache.FinishAttempt(ctx, config.AppID, message.FromUserName, ticket, errors.Is(err, ErrWechatBindingExpired)); finishErr != nil {
			return "", finishErr
		}
	}
	switch {
	case ticket == "" && (errors.Is(err, ErrWechatBindingExpired) || errors.Is(err, ErrWechatBindingRateLimited)):
		return "绑定码没对上次数有点多，我先歇十分钟。已有绑定不受影响，稍后再发新码就行。", nil
	case errors.Is(err, ErrWechatBindingExpired):
		return "绑定码没对上或已过期。回 GoodHR 重新生成，把新码发给我就行。", nil
	case errors.Is(err, ErrWechatFollowRequired):
		return "我还没收到你的关注记录。若之前已关注，请取消后重新关注，再把绑定码发一次。会员只赠送一次，不会重复扣掉已有天数。", nil
	case errors.Is(err, ErrWechatOpenIDConflict):
		return "这个微信已绑定另一个 GoodHR 账号。请回对应账号查看；需要换绑时，先取消关注，再重新关注并使用新账号的绑定码。", nil
	case err != nil:
		return "", err
	case result.GiftGranted:
		return fmt.Sprintf("绑定好了，%d 天会员已到账。已有会员的剩余时间也帮你保留了，回 GoodHR 就能看到。", days), nil
	default:
		return "绑定好了，可以回 GoodHR 继续工作。关注奖励之前已领取过，这次不重复赠送，后续软件通知仍会在这里发布。", nil
	}
}
