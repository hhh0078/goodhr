// 本文件保存公众号登录的短期浏览器凭据与已验证微信身份；生产复用 Redis，临时 OpenID 随登录码到期清除。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const wechatLoginTTL = 5 * time.Minute

// wechatLoginChallenge 将公开短码与浏览器独占凭据分开，只有签名通过的公众号回调才能填入微信身份。
type wechatLoginChallenge struct {
	TicketHash string `json:"ticket_hash"`
	CodeHash   string `json:"code_hash"`
	CreatedAt  int64  `json:"created_at"`
	ExpiresAt  int64  `json:"expires_at"`
	AppID      string `json:"app_id"`
	OpenID     string `json:"open_id,omitempty"`
}

// wechatLoginCache 让 Redis 和无数据库开发模式保持相同的一次消费、期限及首个微信认领规则。
type wechatLoginCache struct {
	client     *redis.Client
	mu         sync.Mutex
	challenges map[string]wechatLoginChallenge
	codes      map[string]string
	now        func() time.Time
}

// newWechatLoginCache 共享认证 Redis，未配置 Redis 时使用有期限的开发缓存，连接故障不降级。
func newWechatLoginCache(auth *AuthService, now func() time.Time) *wechatLoginCache {
	c := &wechatLoginCache{challenges: make(map[string]wechatLoginChallenge), codes: make(map[string]string), now: now}
	if auth != nil {
		if store, ok := auth.store.(*RedisAuthStore); ok {
			c.client = store.client
		}
	}
	return c
}

var wechatLoginCreateScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 or redis.call('EXISTS', KEYS[2]) == 1 then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[3])
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
return 1`)

var wechatLoginApproveScript = redis.NewScript(`
local ticket = redis.call('GET', KEYS[1])
if not ticket then return 0 end
local key = ARGV[1] .. ticket
local raw = redis.call('GET', key)
if not raw then return 0 end
local challenge = cjson.decode(raw)
if challenge.app_id ~= ARGV[2] or tonumber(ARGV[4]) < challenge.created_at or tonumber(ARGV[5]) >= challenge.expires_at then return 0 end
if challenge.open_id and challenge.open_id ~= '' and challenge.open_id ~= ARGV[3] then return 0 end
challenge.open_id = ARGV[3]
redis.call('SET', key, cjson.encode(challenge), 'KEEPTTL')
return 1`)

var wechatLoginTakeScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return '' end
local challenge = cjson.decode(raw)
if tonumber(ARGV[2]) >= challenge.expires_at then return '' end
if ARGV[1] == '2' or (ARGV[1] == '1' and challenge.open_id and challenge.open_id ~= '') then
 redis.call('DEL', KEYS[1])
 local codekey = ARGV[3] .. challenge.code_hash
 if redis.call('GET', codekey) == challenge.ticket_hash then redis.call('DEL', codekey) end
end
return raw`)

const wechatLoginTicketPrefix = "goodhr:wechat-login:ticket:"
const wechatLoginCodePrefix = "goodhr:wechat-login:code:"

// Cancel 只凭原页面的随机凭据作废旧码，重新生成后旧公众号消息不再错误提示登录成功。
func (c *wechatLoginCache) Cancel(ctx context.Context, ticketHash string) error {
	if c.client != nil {
		return wechatLoginTakeScript.Run(ctx, c.client, []string{wechatLoginTicketPrefix + ticketHash}, "2", c.now().Unix(), wechatLoginCodePrefix).Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if value, exists := c.challenges[ticketHash]; exists {
		delete(c.challenges, ticketHash)
		if c.codes[value.CodeHash] == ticketHash {
			delete(c.codes, value.CodeHash)
		}
	}
	return nil
}

// Create 原子占用短码并保存浏览器凭据，碰撞时由调用者重新生成，绝不覆盖其他登录页面。
func (c *wechatLoginCache) Create(ctx context.Context, value wechatLoginChallenge) (bool, error) {
	if c.client != nil {
		data, err := json.Marshal(value)
		if err != nil {
			return false, err
		}
		n, err := wechatLoginCreateScript.Run(ctx, c.client, []string{wechatLoginCodePrefix + value.CodeHash, wechatLoginTicketPrefix + value.TicketHash}, value.TicketHash, string(data), wechatLoginTTL.Milliseconds()).Int()
		return n == 1, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	for hash, old := range c.challenges {
		if old.ExpiresAt <= c.now().Unix() {
			delete(c.challenges, hash)
			if c.codes[old.CodeHash] == hash {
				delete(c.codes, old.CodeHash)
			}
		}
	}
	if _, exists := c.codes[value.CodeHash]; exists {
		return false, nil
	}
	if _, exists := c.challenges[value.TicketHash]; exists {
		return false, nil
	}
	c.challenges[value.TicketHash] = value
	c.codes[value.CodeHash] = value.TicketHash
	return true, nil
}

// Approve 只接受期限内、生成之后的签名消息；重复回调幂等，另一微信不能覆盖已经认领的页面。
func (c *wechatLoginCache) Approve(ctx context.Context, codeHash, appID, openID string, messageAt int64) (bool, error) {
	if c.client != nil {
		n, err := wechatLoginApproveScript.Run(ctx, c.client, []string{wechatLoginCodePrefix + codeHash}, wechatLoginTicketPrefix, appID, openID, messageAt, c.now().Unix()).Int()
		return n == 1, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	hash := c.codes[codeHash]
	value, ok := c.challenges[hash]
	if !ok || value.AppID != appID || value.ExpiresAt <= c.now().Unix() || messageAt < value.CreatedAt || value.OpenID != "" && value.OpenID != openID {
		return false, nil
	}
	value.OpenID = openID
	c.challenges[hash] = value
	return true, nil
}

// Read 只有浏览器的256位凭据能读取确认结果，consume 为真时原子消费已确认挑战，未确认的挑战不会被删除。
func (c *wechatLoginCache) Read(ctx context.Context, ticketHash string, consume bool) (wechatLoginChallenge, error) {
	if c.client != nil {
		flag := "0"
		if consume {
			flag = "1"
		}
		raw, err := wechatLoginTakeScript.Run(ctx, c.client, []string{wechatLoginTicketPrefix + ticketHash}, flag, c.now().Unix(), wechatLoginCodePrefix).Text()
		if err != nil {
			return wechatLoginChallenge{}, err
		}
		if raw == "" {
			return wechatLoginChallenge{}, ErrNotFound
		}
		var value wechatLoginChallenge
		err = json.Unmarshal([]byte(raw), &value)
		return value, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return wechatLoginChallenge{}, err
	}
	value, ok := c.challenges[ticketHash]
	if !ok || value.ExpiresAt <= c.now().Unix() {
		return wechatLoginChallenge{}, ErrNotFound
	}
	if consume && value.OpenID != "" {
		delete(c.challenges, ticketHash)
		if c.codes[value.CodeHash] == ticketHash {
			delete(c.codes, value.CodeHash)
		}
	}
	return value, nil
}

// validWechatLoginTicket 拒绝公开短码或任意文本冒充浏览器的随机登录凭据。
func validWechatLoginTicket(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

var errWechatLoginUnavailable = errors.New("公众号登录暂时没准备好，可以先用邮箱登录")
