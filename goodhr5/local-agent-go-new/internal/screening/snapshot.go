// Package screening 定义岗位候选人两轮筛选的完整快照，不包含登录凭证、租户指定或简历正文。
package screening

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Round 保存一轮实际判定；指针区分未执行、零分和零毫秒，不用零值补造历史。
type Round struct {
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Accepted   *bool      `json:"accepted"`
	Score      *float64   `json:"score"`
	Threshold  *float64   `json:"threshold"`
	Reason     string     `json:"reason"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	DurationMS *int64     `json:"duration_ms"`
}

// Snapshot 是本地持久化与云端POST共用的完整版本；重传保留原编号和内容。
type Snapshot struct {
	AttemptID           string    `json:"attempt_id"`
	TaskID              string    `json:"task_id"`
	PlatformID          string    `json:"platform_id"`
	PlatformCandidateID string    `json:"platform_candidate_id"`
	CandidateKey        string    `json:"candidate_key"`
	CandidateName       string    `json:"candidate_name"`
	OccurredAt          time.Time `json:"occurred_at"`
	Revision            int64     `json:"revision"`
	Completed           bool      `json:"completed"`
	TerminationReason   string    `json:"termination_reason"`
	First               Round     `json:"first"`
	Second              Round     `json:"second"`
}

// NewID 用操作系统随机源创建UUID，不能用姓名或平台指纹合并不同次筛选。
func NewID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("生成筛选编号失败：%w", err)
	}
	data[6] = (data[6] & 15) | 64
	data[8] = (data[8] & 63) | 128
	value := hex.EncodeToString(data[:])
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:], nil
}

// NotExecuted 保留没有实际调用判定的原因，所有数值和时间均为空。
func NotExecuted(reason string) Round {
	return Round{Kind: "none", Status: "not_executed", Reason: reason}
}

// CloseInterrupted 结束未完成快照；异常恢复只保留已知开始，不用恢复时间伪造判定耗时。
func CloseInterrupted(item *Snapshot, reason string) {
	if item.Completed {
		return
	}
	if runes := []rune(reason); len(runes) > 16000 {
		reason = string(runes[:16000])
	}
	for _, round := range []*Round{&item.First, &item.Second} {
		if round.Status == "running" {
			round.Status = "cancelled"
			round.Accepted = nil
			round.Score = nil
			round.Threshold = nil
			round.FinishedAt = nil
			round.DurationMS = nil
			round.Reason = reason
		} else if round.Status == "not_executed" {
			round.Reason = reason
		}
	}
	item.Completed = true
	item.TerminationReason = reason
}
