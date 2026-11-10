// Package failurediag 本文件定义本机附件阶段及回传服务器的白名单，仅携带操作位置和数字，不携带文件或地址。
package failurediag

import (
	"context"
	"errors"
	"time"
)

// AttachmentReceipt 仅传输附件阶段、固定原因和预算，不接受设备提交的自由文本。
type AttachmentReceipt struct {
	Stage     int   `json:"stage"`
	Cause     int   `json:"cause,omitempty"`
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`
	LimitMS   int64 `json:"limit_ms,omitempty"`
	Attempt   int64 `json:"attempt,omitempty"`
}

// 阶段和原因编号用于设备回执，保持追加顺序以兼容老版本。
var attachmentPhases = [...]string{"", "打开附件预览", "触发附件下载", "等待下载开始", "等待下载回执", "核对下载文件", "保存当前附件文档", "读取文件准备回传", "核对附件页面", "准备下载监听", "读取附件预览", "恢复会话页面"}
var attachmentReasons = [...]string{"", "上层请求已取消", "等待时限已到", "尚未观察到下载开始", "多次检查仍无可用文件", "附件步骤未完成", "上层请求时限已到", "下载回执等待时限已到"}

// AttachmentFailure 附加已实际进入的附件阶段；错误类型、文字和原执行策略保持不变。
func AttachmentFailure(err error, phase string, started time.Time, limit time.Duration, attempt int64, reason string) error {
	if err == nil {
		return nil
	}
	if reason == "" {
		switch {
		case errors.Is(err, context.Canceled):
			reason = attachmentReasons[1]
		case errors.Is(err, context.DeadlineExceeded):
			reason = attachmentReasons[2]
		default:
			reason = attachmentReasons[5]
		}
	}
	return Wrap(err, Detail{Source: "简历附件", Operation: "取得附件文件", Phase: phase, Reason: reason, ElapsedMS: time.Since(started).Milliseconds(), LimitMS: limit.Milliseconds(), Attempt: attempt})
}

// AttachmentEvidence 提取能安全随本机命令回传的附件证据，其他模块或未知描述不会写入回执。
func AttachmentEvidence(err error) *AttachmentReceipt {
	for _, d := range Details(err) {
		if d.Source != "简历附件" {
			continue
		}
		for i, phase := range attachmentPhases {
			if i == 0 || phase != d.Phase {
				continue
			}
			r := &AttachmentReceipt{Stage: i, ElapsedMS: d.ElapsedMS, LimitMS: d.LimitMS, Attempt: d.Attempt}
			for j, reason := range attachmentReasons {
				if reason == d.Reason {
					r.Cause = j
					break
				}
			}
			return r
		}
	}
	return nil
}

// Restore 重建受信任的固定附件描述；不接受未知编号或超过单次任务范围的计数。
func (r AttachmentReceipt) Restore(err error) error {
	if r.Stage < 1 || r.Stage >= len(attachmentPhases) || r.Cause < 1 || r.Cause >= len(attachmentReasons) || r.ElapsedMS < 0 || r.ElapsedMS > 86400000 || r.LimitMS < 0 || r.LimitMS > 86400000 || r.Attempt < 0 || r.Attempt > 1000 {
		return err
	}
	return Wrap(err, Detail{Source: "简历附件", Operation: "取得附件文件", Phase: attachmentPhases[r.Stage], Reason: attachmentReasons[r.Cause], ElapsedMS: r.ElapsedMS, LimitMS: r.LimitMS, Attempt: r.Attempt})
}
