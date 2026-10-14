// Package shared 文件作用：用原子状态管理本任务教程事实的单次后台同步，不保存候选人资料。
package shared

import "sync/atomic"

// ScreeningResultSyncState 保证同一任务最多一个教程同步请求在途，成功后不再重复上报。
// Stats 只持有这个状态的指针；后台请求只修改原子字段，不读写主流程统计。
type ScreeningResultSyncState struct {
	busy atomic.Bool
	done atomic.Bool
}

// TryStart 在尚未成功且没有请求在途时领取本次同步，失败后可由后续真实结果重试。
func (s *ScreeningResultSyncState) TryStart() bool {
	if s == nil || s.done.Load() || !s.busy.CompareAndSwap(false, true) {
		return false
	}
	if s.done.Load() {
		s.busy.Store(false)
		return false
	}
	return true
}

// Finish 成功时永久标记完成，失败时释放在途状态，不能把同步失败当成教程完成。
func (s *ScreeningResultSyncState) Finish(success bool) {
	if s == nil {
		return
	}
	if success {
		s.done.Store(true)
	}
	s.busy.Store(false)
}

// Reported 安全读取后台同步是否已成功，不会改变请求状态或招聘统计。
func (s *ScreeningResultSyncState) Reported() bool {
	return s != nil && s.done.Load()
}
