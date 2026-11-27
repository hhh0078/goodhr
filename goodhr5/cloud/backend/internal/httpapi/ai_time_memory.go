// Package httpapi 本文件实现时间套餐的内存账本，期限、独立额度和流水在同一锁中更新。
package httpapi

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// MemoryAITimeStore 保存开发环境中的套餐账期与按订单去重的权益记录。
type MemoryAITimeStore struct {
	mu       sync.Mutex
	wallet   *MemoryAIWalletStore
	accounts map[string]AITimeAccount
	terms    map[string]aiTimeTerms
	grants   map[string]string
}

// NewMemoryAITimeStore 创建独立套餐账本，不转移原按量余额。
func NewMemoryAITimeStore(wallet *MemoryAIWalletStore) *MemoryAITimeStore {
	return &MemoryAITimeStore{wallet: wallet, accounts: map[string]AITimeAccount{}, terms: map[string]aiTimeTerms{}, grants: map[string]string{}}
}

// appendRecordLocked 将套餐流水写入共用消费记录，不修改按量余额和可退本金。
func (s *MemoryAITimeStore) appendRecordLocked(account AITimeAccount, record AIWalletRecord, now time.Time) {
	if record.Category == "ai_usage" {
		record.BonusChangeUnits = record.ChangeUnits
	}
	if record.Category == "admin_adjust" {
		record.Category = "time_package_adjust"
	}
	record.UserEmail = account.UserEmail
	record.BalanceAfterUnits = account.BalanceUnits
	record.BonusBalanceAfterUnits = max(int64(0), account.BalanceUnits)
	record.WalletLedgerVersion = aiWalletLedgerVersion
	record.RelatedOrderNo = account.ID
	record.Reason = timeAccountReason(record.Reason)
	record.CreatedAt = now
	s.wallet.mu.Lock()
	defer s.wallet.mu.Unlock()
	s.wallet.nextRecordID++
	record.ID = fmt.Sprintf("wallet-%d", s.wallet.nextRecordID)
	s.wallet.records = append(s.wallet.records, record)
}

// expireLocked 回收指定旧账期额度，原始 AI 消费流水保持不变。
func (s *MemoryAITimeStore) expireLocked(account AITimeAccount, now time.Time) AITimeAccount {
	if !account.active(now) && account.BalanceUnits != 0 {
		change := -account.BalanceUnits
		account.BalanceUnits = 0
		s.accounts[account.ID] = account
		s.appendRecordLocked(account, AIWalletRecord{ChangeUnits: change, Category: "time_package_expiry", Reason: "套餐到期回收剩余额度"}, now)
	}
	return account
}

// Current 读取本人最新套餐并回收到期额度。
func (s *MemoryAITimeStore) Current(email string, now time.Time) (AITimeAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var current AITimeAccount
	for _, account := range s.accounts {
		if account.UserEmail == email {
			account = s.expireLocked(account, now)
			if current.ID == "" || account.ExpiresAt.After(current.ExpiresAt) {
				current = account
			}
		}
	}
	return current, nil
}

// Account 读取固定资金账期，拒绝跨账号扣费。
func (s *MemoryAITimeStore) Account(email, id string, now time.Time) (AITimeAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[id]
	if !ok || account.UserEmail != email {
		return AITimeAccount{}, errors.New("套餐账期不存在")
	}
	return s.expireLocked(account, now), nil
}

// SaveTerms 在生成支付二维码前保存不可变额度快照。
func (s *MemoryAITimeStore) SaveTerms(order PaymentOrder, terms aiTimeTerms) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if terms.BalanceUnits <= 0 {
		return errors.New("套餐额度必须大于0")
	}
	if previous, ok := s.terms[order.OrderNo]; ok && previous != terms {
		return errors.New("套餐订单额度已固定")
	}
	s.terms[order.OrderNo] = terms
	return nil
}

// Grant 将已支付订单幂等发放到当前账期，续费只补足额度且不减少已有额度。
func (s *MemoryAITimeStore) Grant(order PaymentOrder, now time.Time) (AITimeAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if order.Status != "paid" || order.OrderType != "ai_time" || order.DurationDays <= 0 {
		return AITimeAccount{}, errors.New("套餐订单尚未支付")
	}
	if id, ok := s.grants[order.OrderNo]; ok {
		return s.expireLocked(s.accounts[id], now), nil
	}
	terms, ok := s.terms[order.OrderNo]
	if !ok {
		return AITimeAccount{}, errors.New("套餐订单额度快照缺失")
	}
	var current AITimeAccount
	for _, a := range s.accounts {
		if a.UserEmail == order.UserEmail {
			a = s.expireLocked(a, now)
			if a.active(now) && (current.ID == "" || a.ExpiresAt.After(current.ExpiresAt)) {
				current = a
			}
		}
	}
	if current.ID == "" {
		current = AITimeAccount{ID: order.OrderNo, UserEmail: order.UserEmail, ExpiresAt: now}
	}
	current.PlanID, current.PlanName = order.PlanID, order.PlanName
	current.ExpiresAt = current.ExpiresAt.Add(time.Duration(order.DurationDays) * 24 * time.Hour)
	change, err := timeTopUpUnits(current.BalanceUnits, terms.BalanceUnits)
	if err != nil {
		return AITimeAccount{}, err
	}
	current.BalanceUnits += change
	s.accounts[current.ID] = current
	s.grants[order.OrderNo] = current.ID
	s.appendRecordLocked(current, AIWalletRecord{ChangeUnits: change, Category: "time_package_grant", Reason: order.PlanName + "支付成功：" + order.OrderNo}, now)
	return current, nil
}

// Adjust 对请求开始时固定的账期扣费，跨到期请求完成后仍记录费用再清零，绝不扣按量本金。
func (s *MemoryAITimeStore) Adjust(record AIWalletRecord, id string, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[id]
	if !ok || account.UserEmail != record.UserEmail {
		return 0, errors.New("套餐账期不匹配")
	}
	if record.Category != "ai_usage" && (!account.active(now) || record.Category != "admin_adjust") {
		return 0, errors.New("套餐已到期，不能再调整额度")
	}
	next, err := checkedAIWalletAdd(account.BalanceUnits, record.ChangeUnits)
	if err != nil || next == -1<<63 {
		return 0, errors.New("套餐额度超出可记账范围")
	}
	account.BalanceUnits = next
	s.accounts[id] = account
	s.appendRecordLocked(account, record, now)
	account = s.expireLocked(account, now)
	return account.BalanceUnits, nil
}

// Expire 定期回收所有旧账期，关闭页面也不会让过期额度继续存在。
func (s *MemoryAITimeStore) Expire(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, account := range s.accounts {
		s.expireLocked(account, now)
	}
	return nil
}
