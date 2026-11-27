// Package shared 本文件验证会话恢复错误与窗口检查错误交替出现时仍能停止，其他候选人成功不能冲掉失败次数。
package shared

import (
	"errors"
	"testing"
)

// TestScopedFailuresSurviveInterleaving 覆盖线上交替报错和间歇处理成功导致永不停止的回归。
func TestScopedFailuresSurviveInterleaving(t *testing.T) {
	p := &ConsecutiveErrorPolicy{}
	for i := 0; i < 3; i++ {
		err := p.RecordFor("conversation:stuck", errors.New([]string{"历史读取失败", "身份尚未核实", "历史依然未读完整"}[i]))
		if (i == 2) != (err != nil) {
			t.Fatalf("第%d轮没有按对象执行上限：%v", i+1, err)
		}
		if i < 2 {
			if err := p.RecordFor("open_window", errors.New("当前聊天已经切换")); err != nil {
				t.Fatal(err)
			}
			p.ResetFor("conversation:other")
		}
	}
	if !p.HasErrors() {
		t.Fatal("别的会话成功清掉了待解决错误")
	}
	p.ResetFor("conversation:stuck")
	p.ResetFor("open_window")
	if p.HasErrors() {
		t.Fatal("成功恢复后未清除对象错误")
	}
}
