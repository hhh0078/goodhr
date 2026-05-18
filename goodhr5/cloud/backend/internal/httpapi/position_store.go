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
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PositionStore 定义岗位配置的持久化能力。
type PositionStore interface {
	ListPositions(userEmail string) ([]Position, error)
	SavePosition(position Position) (Position, error)
	PositionByID(userEmail string, positionID string) (Position, error)
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

// ListPositions 列出当前用户的岗位配置。
func (s *MemoryPositionStore) ListPositions(userEmail string) ([]Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]Position, 0)
	for _, item := range s.positions {
		if item.UserEmail != userEmail {
			continue
		}
		items = append(items, item)
	}
	return items, nil
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
func (s *MemoryPositionStore) PositionByID(userEmail string, positionID string) (Position, error) {
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
