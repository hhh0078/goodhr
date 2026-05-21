// 本文件负责定义岗位配置的数据模型和存储接口。
package httpapi

import (
	"sync"
	"time"
)

// Position 表示一个用户可复用的岗位筛选配置。
type Position struct {
	ID              string
	UserEmail       string
	Name            string
	Keywords        []string
	ExcludeKeywords []string
	Description     string
	GreetMessage    string
	IsAndMode       bool
	CommonConfig    map[string]any
	AIConfig        map[string]any
	KeywordConfig   map[string]any
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PositionStore 定义岗位配置的持久化能力。
type PositionStore interface {
	ListPositions(tenantID, userEmail string, isAdmin bool) ([]Position, error)
	SavePosition(position Position) (Position, error)
	PositionByID(tenantID, userEmail, positionID string, isAdmin bool) (Position, error)
	DeletePosition(userEmail string, positionID string) error
}

// MemoryPositionStore 提供开发期使用的内存岗位配置存储。
type MemoryPositionStore struct {
	mu        sync.Mutex
	positions map[string]Position
	now       func() time.Time
	nextID    func() string
}

// NewMemoryPositionStore 创建开发期内存岗位配置存储。
func NewMemoryPositionStore() *MemoryPositionStore {
	seq := 0
	return &MemoryPositionStore{
		positions: make(map[string]Position),
		now:       time.Now,
		nextID: func() string {
			seq++
			return "position_" + intString(seq)
		},
	}
}

// SavePosition 保存一个岗位配置。
func (s *MemoryPositionStore) SavePosition(position Position) (Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if position.ID == "" {
		position.ID = s.nextID()
		position.CreatedAt = now
	}
	position.UpdatedAt = now
	s.positions[position.ID] = position
	return position, nil
}

// PositionByID 读取当前用户的单个岗位配置。
func (s *MemoryPositionStore) ListPositions(tenantID, userEmail string, isAdmin bool) ([]Position, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	items := make([]Position, 0)
	for _, p := range s.positions {
		if isAdmin { items = append(items, p) } else if p.UserEmail == userEmail { items = append(items, p) }
	}
	return items, nil
}
func (s *MemoryPositionStore) PositionByID(tenantID, userEmail, positionID string, isAdmin bool) (Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.positions[positionID]
	if !ok || item.UserEmail != userEmail {
		return Position{}, ErrNotFound
	}
	return item, nil
}

// DeletePosition 删除当前用户的岗位配置。
func (s *MemoryPositionStore) DeletePosition(userEmail string, positionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.positions[positionID]
	if !ok || item.UserEmail != userEmail {
		return ErrNotFound
	}

	delete(s.positions, positionID)
	return nil
}
