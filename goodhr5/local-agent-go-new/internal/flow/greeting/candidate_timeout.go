// Package greeting 文件作用：定义候选人五分钟处理上限，并单独记录达到总时限的错误。
package greeting

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const candidateTimeout = 5 * time.Minute

var errCandidateProcessingTimeout = errors.New("候选人处理超时（已达到5分钟上限）")

// finishCandidateProcessing 仅标记候选人自身总时限，保留原始故障和完整耗时，避免误报用户停止或单步超时。
func (f *Flow) finishCandidateProcessing(ctx context.Context, taskID string, startedAt time.Time, err error) error {
	if !errors.Is(context.Cause(ctx), errCandidateProcessingTimeout) {
		return err
	}
	if err == nil {
		err = context.DeadlineExceeded
	}
	err = fmt.Errorf("%w：%w", errCandidateProcessingTimeout, err)
	f.log(taskID, "candidate_timeout", "failed", startedAt, err)
	return err
}
