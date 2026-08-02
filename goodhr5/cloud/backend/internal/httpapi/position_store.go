// 本文件负责定义岗位配置的数据模型和存储接口。
package httpapi

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrPositionAlreadyRunning 表示当前账号已经有岗位任务处于运行状态。
var ErrPositionAlreadyRunning = errors.New("position already running")

// Position 表示一个用户可复用的岗位筛选配置。
type Position struct {
	ID                string
	UserEmail         string
	PlatformID        string
	Name              string
	Keywords          []string
	ExcludeKeywords   []string
	Description       string
	GreetMessage      string
	IsAndMode         bool
	CommonConfig      map[string]any
	AIConfig          map[string]any
	KeywordConfig     map[string]any
	MatchLimit        int
	Status            string
	ScannedCount      int
	DailyGreetedCount int
	DailyGreetedDate  string
	SkippedCount      int
	FailedCount       int
	EnableSound       bool
	EnableThinking    bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
}

// UserFlowProgress 表示从岗位真实统计中推导出的首次流程进度。
type UserFlowProgress struct {
	ProcessedResume bool
	GreetedToday    bool
}

// PositionStore 定义岗位配置的持久化能力。
type PositionStore interface {
	ListPositions(tenantID, userEmail string, isAdmin bool) ([]Position, error)
	SavePosition(position Position) (Position, error)
	PositionByID(tenantID, userEmail, positionID string, isAdmin bool) (Position, error)
	DeletePosition(userEmail string, positionID string) error
	ClaimPositionStart(userEmail, positionID string) error
	UpdatePositionStatus(positionID, status string) error
	FinishPositionRun(positionID, status string, greeted int) error
	IncrementPositionCounts(positionID string, scanned, skipped, failed int) error
	SyncPositionCounts(positionID string, scanned, skipped, failed int) error
	TodayGreetedTotal() (int, error)
	UserFlowProgress(userEmail string) (UserFlowProgress, error)
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
		position.Status = "created"
		position.DailyGreetedDate = positionBusinessDate(now)
	} else if existing, ok := s.positions[position.ID]; ok {
		position.Status = existing.Status
		position.ScannedCount = existing.ScannedCount
		position.DailyGreetedCount = existing.DailyGreetedCount
		position.DailyGreetedDate = existing.DailyGreetedDate
		position.SkippedCount = existing.SkippedCount
		position.FailedCount = existing.FailedCount
		position.StartedAt = existing.StartedAt
		position.FinishedAt = existing.FinishedAt
		position.CreatedAt = existing.CreatedAt
	}
	position.UpdatedAt = now
	s.positions[position.ID] = position
	return position, nil
}

// IncrementPositionCounts 累加内存岗位统计。
// positionID 为岗位 ID，其余参数为本次新增的扫描、跳过和失败数量。
func (s *MemoryPositionStore) IncrementPositionCounts(positionID string, scanned, skipped, failed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	position, ok := s.positions[positionID]
	if !ok {
		return ErrNotFound
	}
	position.ScannedCount += maxIntValue(0, scanned)
	position.SkippedCount += maxIntValue(0, skipped)
	position.FailedCount += maxIntValue(0, failed)
	position.UpdatedAt = s.now()
	s.positions[positionID] = position
	return nil
}

// ClaimPositionStart 在同一把内存锁内检查账号运行冲突并把目标岗位改为运行中。
// userEmail 为岗位所属账号，positionID 为本次申请启动的岗位编号。
func (s *MemoryPositionStore) ClaimPositionStart(userEmail, positionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	position, ok := s.positions[positionID]
	if !ok || position.UserEmail != userEmail {
		return ErrNotFound
	}
	for _, item := range s.positions {
		if item.UserEmail == userEmail && item.Status == "running" {
			return ErrPositionAlreadyRunning
		}
	}
	now := s.now()
	position.Status = "running"
	position.StartedAt = &now
	position.FinishedAt = nil
	position.UpdatedAt = now
	s.positions[positionID] = position
	return nil
}

// UpdatePositionStatus 更新岗位当前运行状态。
// positionID 为岗位 ID，status 为新的运行状态。
func (s *MemoryPositionStore) UpdatePositionStatus(positionID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	position, ok := s.positions[positionID]
	if !ok {
		return ErrNotFound
	}
	now := s.now()
	position.Status = status
	position.UpdatedAt = now
	if status == "running" {
		position.StartedAt = &now
		position.FinishedAt = nil
	} else if status == "completed" || status == "stopped" || status == "failed" {
		position.FinishedAt = &now
	}
	s.positions[positionID] = position
	return nil
}

// FinishPositionRun 幂等保存岗位结束状态并累加本次打招呼数量。
// positionID 为岗位 ID，status 为结束状态，greeted 为本次打招呼数量。
func (s *MemoryPositionStore) FinishPositionRun(positionID, status string, greeted int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	position, ok := s.positions[positionID]
	if !ok {
		return ErrNotFound
	}
	if position.Status == status {
		return nil
	}
	now := s.now()
	today := positionBusinessDate(now)
	if position.DailyGreetedDate != today {
		position.DailyGreetedDate = today
		position.DailyGreetedCount = 0
	}
	position.DailyGreetedCount += maxIntValue(0, greeted)
	position.Status = status
	position.FinishedAt = &now
	position.UpdatedAt = now
	s.positions[positionID] = position
	return nil
}

// SyncPositionCounts 按本地累计值同步岗位统计。
// positionID 为岗位 ID，其余参数为累计扫描、跳过和失败数量。
func (s *MemoryPositionStore) SyncPositionCounts(positionID string, scanned, skipped, failed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	position, ok := s.positions[positionID]
	if !ok {
		return ErrNotFound
	}
	position.ScannedCount = maxIntValue(position.ScannedCount, scanned)
	position.SkippedCount = maxIntValue(position.SkippedCount, skipped)
	position.FailedCount = maxIntValue(position.FailedCount, failed)
	position.UpdatedAt = s.now()
	s.positions[positionID] = position
	return nil
}

// TodayGreetedTotal 汇总所有岗位今天的打招呼数量。
func (s *MemoryPositionStore) TodayGreetedTotal() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := positionBusinessDate(s.now())
	total := 0
	for _, position := range s.positions {
		if position.DailyGreetedDate == today {
			total += position.DailyGreetedCount
		}
	}
	return total, nil
}

// UserFlowProgress 返回指定账号是否处理过简历以及今天是否成功打过招呼。
func (s *MemoryPositionStore) UserFlowProgress(userEmail string) (UserFlowProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress := UserFlowProgress{}
	today := positionBusinessDate(s.now())
	for _, position := range s.positions {
		if !strings.EqualFold(position.UserEmail, userEmail) {
			continue
		}
		progress.ProcessedResume = progress.ProcessedResume || position.ScannedCount > 0
		progress.GreetedToday = progress.GreetedToday ||
			(position.DailyGreetedDate == today && position.DailyGreetedCount > 0)
	}
	return progress, nil
}

// positionBusinessDate 返回 GoodHR 业务使用的北京时间日期。
// now 为任意时区的当前时间，返回 YYYY-MM-DD 格式日期。
func positionBusinessDate(now time.Time) string {
	return now.In(chinaLocation()).Format(time.DateOnly)
}

// maxIntValue 返回两个整数中的较大值。
func maxIntValue(left, right int) int {
	if left > right {
		return left
	}
	return right
}

// PositionByID 读取当前用户的单个岗位配置。
func (s *MemoryPositionStore) ListPositions(tenantID, userEmail string, isAdmin bool) ([]Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Position, 0)
	for _, p := range s.positions {
		if isAdmin {
			items = append(items, p)
		} else if p.UserEmail == userEmail {
			items = append(items, p)
		}
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
