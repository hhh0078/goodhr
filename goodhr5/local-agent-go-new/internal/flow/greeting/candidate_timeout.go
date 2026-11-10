// Package greeting 文件作用：定义候选人五分钟处理上限，并单独记录达到总时限的错误。
package greeting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

const candidateTimeout = 5 * time.Minute

// ErrCandidateProcessingTimeout 标识仅由单个候选人自身五分钟计时器触发的截止，供终态日志辨别真实来源。
var ErrCandidateProcessingTimeout = errors.New("候选人处理超时（已达到5分钟上限）")

// finishCandidateProcessing 仅标记候选人自身总时限，保留原始故障和完整耗时，避免误报用户停止或单步超时。
func (f *Flow) finishCandidateProcessing(ctx context.Context, taskID string, startedAt time.Time, err error) error {
	if !errors.Is(context.Cause(ctx), ErrCandidateProcessingTimeout) {
		return err
	}
	if err == nil {
		err = context.DeadlineExceeded
	}
	err = fmt.Errorf("%w：%w", ErrCandidateProcessingTimeout, err)
	err = failurediag.Wrap(err, failurediag.Detail{Source: "岗位流程", Operation: "处理单个候选人", Phase: "候选人总时限", Reason: "单个候选人已用完五分钟预算", ElapsedMS: time.Since(startedAt).Milliseconds(), LimitMS: candidateTimeout.Milliseconds()})
	f.log(taskID, "candidate_timeout", "failed", startedAt, err)
	return err
}
