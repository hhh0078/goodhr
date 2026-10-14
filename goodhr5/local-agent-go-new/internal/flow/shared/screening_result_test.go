// Package shared 文件作用：验证教程同步的并发领取、失败重试及成功去重，不改变主流程统计。
package shared

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestScreeningResultSyncStateConcurrentClaims 验证同时领取仍只有一个请求，并允许失败后重试且成功后永久去重。
func TestScreeningResultSyncStateConcurrentClaims(t *testing.T) {
	state := &ScreeningResultSyncState{}
	var claims atomic.Int32
	var ready sync.WaitGroup
	for index := 0; index < 32; index++ {
		ready.Add(1)
		go func() {
			defer ready.Done()
			if state.TryStart() {
				claims.Add(1)
			}
		}()
	}
	ready.Wait()
	if claims.Load() != 1 || state.Reported() {
		t.Fatalf("在途领取不正确：领取 %d，完成 %t", claims.Load(), state.Reported())
	}
	state.Finish(false)
	if !state.TryStart() {
		t.Fatal("同步失败后不能重试")
	}
	state.Finish(true)
	if !state.Reported() || state.TryStart() {
		t.Fatal("同步成功后仍能重复领取")
	}
	var missing *ScreeningResultSyncState
	if missing.TryStart() || missing.Reported() {
		t.Fatal("空状态不应开始或完成")
	}
	missing.Finish(false)
}
