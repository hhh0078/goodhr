// 本文件负责定义招聘平台账号映射的数据模型和存储接口。
package httpapi

import (
	"errors"
	"sync"
	"time"
)

var ErrConflict = errors.New("conflict")

// PlatformAccount 表示云端可见的平台账号映射，不包含 cookie/profile 原文。
type PlatformAccount struct {
	ID             string
	UserEmail      string
	PlatformID     string
	DisplayName    string
	LocalProfileID string
	CreatedAt      time.Time
}

// PlatformAccountStore 定义平台账号映射的持久化能力。
type PlatformAccountStore interface {
	ListPlatformAccounts(userEmail string, platformID string) ([]PlatformAccount, error)
	SavePlatformAccount(account PlatformAccount) (PlatformAccount, error)
	DeletePlatformAccount(userEmail string, accountID string) error
}

// MemoryPlatformAccountStore 提供开发期使用的内存平台账号映射存储。
type MemoryPlatformAccountStore struct {
	mu       sync.Mutex
	accounts map[string]PlatformAccount
	now      func() time.Time
	nextID   func() string
}

// NewMemoryPlatformAccountStore 创建开发期内存平台账号映射存储。
func NewMemoryPlatformAccountStore() *MemoryPlatformAccountStore {
	seq := 0
	return &MemoryPlatformAccountStore{
		accounts: make(map[string]PlatformAccount),
		now:      time.Now,
		nextID: func() string {
			seq++
			return "platform_account_" + intString(seq)
		},
	}
}

// ListPlatformAccounts 按用户和平台列出平台账号映射。
func (s *MemoryPlatformAccountStore) ListPlatformAccounts(userEmail string, platformID string) ([]PlatformAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]PlatformAccount, 0)
	for _, account := range s.accounts {
		if account.UserEmail != userEmail {
			continue
		}
		if platformID != "" && account.PlatformID != platformID {
			continue
		}
		items = append(items, account)
	}
	return items, nil
}

// SavePlatformAccount 保存平台账号映射，并避免同平台同 profile 重复创建。
func (s *MemoryPlatformAccountStore) SavePlatformAccount(account PlatformAccount) (PlatformAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, saved := range s.accounts {
		if saved.UserEmail == account.UserEmail &&
			saved.PlatformID == account.PlatformID &&
			saved.LocalProfileID == account.LocalProfileID {
			return PlatformAccount{}, ErrConflict
		}
	}

	account.ID = s.nextID()
	account.CreatedAt = s.now()
	s.accounts[account.ID] = account
	return account, nil
}

// DeletePlatformAccount 删除当前用户名下的平台账号映射。
func (s *MemoryPlatformAccountStore) DeletePlatformAccount(userEmail string, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.accounts[accountID]
	if !ok || account.UserEmail != userEmail {
		return ErrNotFound
	}

	delete(s.accounts, accountID)
	return nil
}

// intString 将整数转换为字符串，避免平台账号 ID 生成逻辑散落在调用点。
func intString(value int) string {
	if value == 0 {
		return "0"
	}

	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
