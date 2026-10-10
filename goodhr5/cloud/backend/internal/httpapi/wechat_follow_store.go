// 本文件定义公众号关注绑定与一次性会员奖励存储，并提供不连接数据库的内存实现。
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrWechatBindingExpired      = errors.New("绑定码已经过期，请回网站重新生成")
	ErrWechatFollowRequired      = errors.New("我还没确认关注状态，请取消关注后重新关注，再发送绑定码")
	ErrWechatOpenIDConflict      = errors.New("这个微信或网站账号已经绑定过，请先取消关注再重新绑定")
	ErrWechatBindingCodeConflict = errors.New("这个绑定码暂时有人使用，请重新生成")
	ErrWechatBindingRateLimited  = errors.New("今天这会儿有点忙，请十分钟后再试")
)

// WechatBindingStatus 表示用户当前绑定状态；OpenID 仅供后端内部使用。
type WechatBindingStatus struct {
	Bound       bool   `json:"bound"`
	GiftGranted bool   `json:"gift_granted"`
	GiftDays    int    `json:"gift_days"`
	Followed    bool   `json:"followed"`
	OpenID      string `json:"-"`
}

// WechatBindResult 表示本次绑定是否重放以及是否实际发放了新奖励。
type WechatBindResult struct {
	AlreadyBound bool
	GiftGranted  bool
	Subscription Subscription
}

// WechatBindOptions 指定本次动态奖励、微信消息时间与限流后的幂等重放限制。
type WechatBindOptions struct {
	RewardDays  int
	MessageTime time.Time
	ReplayOnly  bool
}

// WechatFollowStore 定义一次性绑定码、关注事件和幂等奖励的持久化边界。
type WechatFollowStore interface {
	// CreateBinding 保存绑定码摘要，并使同邮箱以前未完成的绑定码失效。
	CreateBinding(ctx context.Context, email, codeHash string, expiresAt time.Time) error
	// BindingStatus 读取本人当前绑定与历史奖励状态。
	BindingStatus(ctx context.Context, email string) (WechatBindingStatus, error)
	// RecordFollow 按事件时间记录关注状态，取消关注时清理当前原始 OpenID。
	RecordFollow(ctx context.Context, appID, openID string, followed bool, eventAt time.Time) error
	// BindCode 原子消费绑定码、绑定当前微信，并最多赠送一次会员。
	BindCode(ctx context.Context, appID, openID, codeHash string, now time.Time, options ...WechatBindOptions) (WechatBindResult, error)
}

// wechatFollowState 只保存公众号用户指纹、关注状态和最后有效事件时间。
type wechatFollowState struct {
	followed bool
	eventAt  time.Time
}

// wechatPendingBinding 保存绑定码摘要以及完成回调的公众号和指纹，不保存回调原始 OpenID。
type wechatPendingBinding struct {
	email       string
	createdAt   time.Time
	expiresAt   time.Time
	invalidated bool
	completed   bool
	appID       string
	fingerprint string
}

// wechatCurrentBinding 对应 users 上的当前绑定，取消关注后直接删除原始 OpenID。
type wechatCurrentBinding struct {
	appID  string
	openID string
}

// MemoryWechatFollowStore 用同一把锁保护关注、挑战和奖励，适合本地开发与单元测试。
type MemoryWechatFollowStore struct {
	mu            sync.Mutex
	subscriptions *MemorySubscriptionStore
	pending       map[string]wechatPendingBinding
	follows       map[string]wechatFollowState
	bindings      map[string]wechatCurrentBinding
	giftEmails    map[string]int
	giftOpenIDs   map[string]struct{}
}

// NewMemoryWechatFollowStore 创建内存公众号存储，并共享现有内存会员数据。
func NewMemoryWechatFollowStore(subscriptions *MemorySubscriptionStore) *MemoryWechatFollowStore {
	if subscriptions == nil {
		subscriptions = NewMemorySubscriptionStore()
	}
	return &MemoryWechatFollowStore{
		subscriptions: subscriptions,
		pending:       make(map[string]wechatPendingBinding),
		follows:       make(map[string]wechatFollowState),
		bindings:      make(map[string]wechatCurrentBinding),
		giftEmails:    make(map[string]int),
		giftOpenIDs:   make(map[string]struct{}),
	}
}

// CreateBinding 保存新的码摘要，旧码失效与新码创建在同一锁内完成。
func (s *MemoryWechatFollowStore) CreateBinding(ctx context.Context, email, codeHash string, expiresAt time.Time) error {
	email, codeHash, err := normalizeWechatBindingInput(email, codeHash)
	if err != nil {
		return err
	}
	if expiresAt.IsZero() {
		return ErrWechatBindingExpired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	createdAt := wechatBindingCreatedAt(expiresAt)
	if old, exists := s.pending[codeHash]; exists && old.expiresAt.After(createdAt) {
		return ErrWechatBindingCodeConflict
	}
	for hash, pending := range s.pending {
		if pending.email == email && !pending.completed && pending.expiresAt.After(createdAt) {
			pending.invalidated = true
			s.pending[hash] = pending
		}
	}
	s.pending[codeHash] = wechatPendingBinding{email: email, createdAt: createdAt, expiresAt: expiresAt}
	return nil
}

// BindingStatus 返回当前绑定和永久领取记录，取消关注不会丢失领取状态。
func (s *MemoryWechatFollowStore) BindingStatus(ctx context.Context, email string) (WechatBindingStatus, error) {
	email, ok := normalizeEmail(email)
	if !ok {
		return WechatBindingStatus{}, errors.New("账号邮箱不正确")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return WechatBindingStatus{}, err
	}
	days, granted := s.giftEmails[email]
	status := WechatBindingStatus{GiftGranted: granted, GiftDays: days}
	if binding, exists := s.bindings[email]; exists {
		status.Bound = true
		status.OpenID = binding.openID
		status.Followed = s.follows[wechatFollowKey(binding.appID, binding.openID)].followed
	}
	return status, nil
}

// RecordFollow 忽略旧事件；同秒冲突优先取消关注，避免延迟事件错误恢复绑定。
func (s *MemoryWechatFollowStore) RecordFollow(ctx context.Context, appID, openID string, followed bool, eventAt time.Time) error {
	appID, openID, err := normalizeWechatIdentity(appID, openID)
	if err != nil {
		return err
	}
	if eventAt.IsZero() {
		return errors.New("关注事件缺少时间")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	key := wechatFollowKey(appID, openID)
	if current, exists := s.follows[key]; exists && !wechatFollowEventIsNewer(current, followed, eventAt) {
		return nil
	}
	s.follows[key] = wechatFollowState{followed: followed, eventAt: eventAt}
	if !followed {
		for email, binding := range s.bindings {
			if binding.appID == appID && binding.openID == openID {
				delete(s.bindings, email)
			}
		}
	}
	return nil
}

// BindCode 在同一锁内验证关注、消费码与更新会员，错误请求不会消费有效码。
func (s *MemoryWechatFollowStore) BindCode(ctx context.Context, appID, openID, codeHash string, now time.Time, options ...WechatBindOptions) (WechatBindResult, error) {
	appID, openID, codeHash, err := normalizeWechatBindCodeInput(appID, openID, codeHash, now)
	if err != nil {
		return WechatBindResult{}, err
	}
	option, err := normalizeWechatBindOptions(options)
	if err != nil {
		return WechatBindResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return WechatBindResult{}, err
	}
	pending, exists := s.pending[codeHash]
	if !exists || pending.invalidated || !now.Before(pending.expiresAt) || wechatMessagePredatesBinding(option.MessageTime, pending.createdAt) {
		return WechatBindResult{}, ErrWechatBindingExpired
	}
	fingerprint := wechatOpenIDFingerprint(openID)
	currentBinding, alreadyBound := s.bindings[pending.email]
	if pending.completed && (pending.appID != appID || pending.fingerprint != fingerprint || !alreadyBound || currentBinding.appID != appID || currentBinding.openID != openID) {
		return WechatBindResult{}, ErrWechatBindingExpired
	}
	if option.ReplayOnly && !pending.completed {
		return WechatBindResult{}, ErrWechatBindingRateLimited
	}
	key := wechatFollowKey(appID, openID)
	if !s.follows[key].followed {
		return WechatBindResult{}, ErrWechatFollowRequired
	}
	if alreadyBound && (currentBinding.appID != appID || currentBinding.openID != openID) {
		return WechatBindResult{}, ErrWechatOpenIDConflict
	}
	for email, binding := range s.bindings {
		if email != pending.email && binding.appID == appID && binding.openID == openID {
			return WechatBindResult{}, ErrWechatOpenIDConflict
		}
	}
	s.subscriptions.grantMu.Lock()
	defer s.subscriptions.grantMu.Unlock()
	current, err := s.subscriptions.UserSubscription(pending.email)
	if err != nil {
		return WechatBindResult{}, err
	}
	if pending.completed {
		return WechatBindResult{AlreadyBound: true, Subscription: current}, nil
	}
	_, emailGranted := s.giftEmails[pending.email]
	_, openIDGranted := s.giftOpenIDs[key]
	granted := !emailGranted && !openIDGranted
	if granted {
		current = calculateWechatFollowGift(current, now, option.RewardDays)
		s.subscriptions.items[pending.email] = current
		s.giftEmails[pending.email] = option.RewardDays
		s.giftOpenIDs[key] = struct{}{}
	}
	s.bindings[pending.email] = wechatCurrentBinding{appID: appID, openID: openID}
	pending.completed = true
	pending.appID = appID
	pending.fingerprint = fingerprint
	s.pending[codeHash] = pending
	return WechatBindResult{AlreadyBound: alreadyBound, GiftGranted: granted, Subscription: current}, nil
}

// normalizeWechatBindingInput 校验并规范化邮箱和 SHA256 摘要，不接受原始绑定码入库。
func normalizeWechatBindingInput(email, codeHash string) (string, string, error) {
	email, ok := normalizeEmail(email)
	if !ok {
		return "", "", errors.New("账号邮箱不正确")
	}
	codeHash, err := normalizeWechatCodeHash(codeHash)
	return email, codeHash, err
}

// normalizeWechatCodeHash 仅接受完整 SHA256 十六进制字符串，并统一为小写。
func normalizeWechatCodeHash(codeHash string) (string, error) {
	codeHash = strings.ToLower(strings.TrimSpace(codeHash))
	if len(codeHash) != sha256.Size*2 {
		return "", ErrWechatBindingExpired
	}
	if _, err := hex.DecodeString(codeHash); err != nil {
		return "", ErrWechatBindingExpired
	}
	return codeHash, nil
}

// normalizeWechatIdentity 校验公众号和微信用户身份，保持 OpenID 大小写不变。
func normalizeWechatIdentity(appID, openID string) (string, string, error) {
	appID, openID = strings.TrimSpace(appID), strings.TrimSpace(openID)
	if appID == "" || openID == "" {
		return "", "", errors.New("公众号或微信用户信息为空")
	}
	return appID, openID, nil
}

// normalizeWechatBindCodeInput 校验绑定回调所需身份、摘要和执行时间。
func normalizeWechatBindCodeInput(appID, openID, codeHash string, now time.Time) (string, string, string, error) {
	appID, openID, err := normalizeWechatIdentity(appID, openID)
	if err != nil {
		return "", "", "", err
	}
	codeHash, err = normalizeWechatCodeHash(codeHash)
	if err != nil {
		return "", "", "", err
	}
	if now.IsZero() {
		return "", "", "", errors.New("绑定请求缺少时间")
	}
	return appID, openID, codeHash, nil
}

// wechatOpenIDFingerprint 计算不可逆用户指纹，取消关注后账本不保留原始 OpenID。
func wechatOpenIDFingerprint(openID string) string {
	sum := sha256.Sum256([]byte(openID))
	return hex.EncodeToString(sum[:])
}

// wechatFollowKey 将公众号与用户指纹组合，避免不同公众号的用户身份串用。
func wechatFollowKey(appID, openID string) string {
	return appID + "\x00" + wechatOpenIDFingerprint(openID)
}

// wechatFollowEventIsNewer 判断事件是否更新，同秒关注与取消冲突时保留取消状态。
func wechatFollowEventIsNewer(current wechatFollowState, followed bool, eventAt time.Time) bool {
	return eventAt.After(current.eventAt) || (eventAt.Equal(current.eventAt) && current.followed && !followed)
}

// wechatBindingCreatedAt 从固定有效期还原创建时间，消息比较时另按微信的秒精度处理。
func wechatBindingCreatedAt(expiresAt time.Time) time.Time {
	return expiresAt.Add(-wechatBindingTTL).UTC()
}

// wechatMessagePredatesBinding 拒绝回收短码之前发出的迟到消息，同一秒新消息可以正常绑定。
func wechatMessagePredatesBinding(messageTime, createdAt time.Time) bool {
	return !messageTime.IsZero() && messageTime.Unix() < createdAt.Unix()
}

// normalizeWechatBindOptions 保持旧调用默认两天，并拒绝越界或重复传入的奖励参数。
func normalizeWechatBindOptions(options []WechatBindOptions) (WechatBindOptions, error) {
	option := WechatBindOptions{RewardDays: defaultWechatFollowRewardDays}
	if len(options) > 1 {
		return option, errors.New("绑定参数重复了，请重新尝试")
	}
	if len(options) == 1 {
		option = options[0]
		if option.RewardDays == 0 {
			option.RewardDays = defaultWechatFollowRewardDays
		}
	}
	if option.RewardDays < 1 || option.RewardDays > 365 {
		return option, errors.New("关注赠送天数需在1到365天之间")
	}
	return option, nil
}

// calculateWechatFollowGift 保留有效付费会员类型与剩余时间，其他用户从现在获得配置天数的 Max。
func calculateWechatFollowGift(current Subscription, now time.Time, days ...int) Subscription {
	rewardDays := defaultWechatFollowRewardDays
	if len(days) > 0 {
		rewardDays = days[0]
	}
	memberType := normalizeMemberType(current.MemberType)
	if (memberType == memberTypePlus || memberType == memberTypeMax) && current.ExpiresAt.After(now) {
		return applySubscriptionGrant(current, SubscriptionGrant{MemberType: memberType, Days: rewardDays}, now)
	}
	return applySubscriptionGrant(current, SubscriptionGrant{MemberType: memberTypeMax, Days: rewardDays, ReplaceFromNow: true}, now)
}
