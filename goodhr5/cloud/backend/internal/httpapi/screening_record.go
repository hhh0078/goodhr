// 本文件定义岗位两轮筛选的完整快照、可空数值、固定分页和跨版本身份校验，不依赖云端简历入库。
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const screeningRecordPageSize = 20

var (
	ErrScreeningRecordInput    = errors.New("筛选记录内容需要调整")
	ErrScreeningRecordConflict = errors.New("同一筛选版本的内容或候选人身份不一致，请保留本地记录后核对")
	screeningUUIDPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ScreeningRoundSnapshot 保存一轮真实判定；nil与实际零分、零毫秒必须始终区分。
type ScreeningRoundSnapshot struct {
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

// ScreeningSnapshot 是本地持久化后提交的完整快照，不允许客户端决定租户或岗位所有者。
type ScreeningSnapshot struct {
	AttemptID           string                 `json:"attempt_id"`
	TaskID              string                 `json:"task_id"`
	PlatformID          string                 `json:"platform_id"`
	PlatformCandidateID string                 `json:"platform_candidate_id"`
	CandidateKey        string                 `json:"candidate_key"`
	CandidateName       string                 `json:"candidate_name"`
	OccurredAt          time.Time              `json:"occurred_at"`
	Revision            int64                  `json:"revision"`
	Completed           bool                   `json:"completed"`
	TerminationReason   string                 `json:"termination_reason"`
	First               ScreeningRoundSnapshot `json:"first"`
	Second              ScreeningRoundSnapshot `json:"second"`
}

// ScreeningRecord 只公开业务快照及显示所需元数据，内部租户与所有者字段不进入响应。
type ScreeningRecord struct {
	ScreeningSnapshot
	ID         string    `json:"id"`
	PositionID string    `json:"position_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ScreeningScope 只由服务端正常登录会话与本人岗位生成，不接受payload覆盖。
type ScreeningScope struct{ TenantID, PositionID, UserEmail string }

// ScreeningRecordQuery 使用固定字段过滤，两轮空分数与空开始时间不会命中范围比较。
type ScreeningRecordQuery struct {
	Page                                                                 int
	FirstScoreMin, FirstScoreMax, SecondScoreMin, SecondScoreMax         *float64
	FirstStartedFrom, FirstStartedTo, SecondStartedFrom, SecondStartedTo *time.Time
}

// ScreeningRecordList 返回固定20条一页的记录与数据库过滤后的总数。
type ScreeningRecordList struct {
	Records  []ScreeningRecord `json:"records"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

// ScreeningRecordStore 按可信岗位范围保存递增快照及查询，旧版本不能覆盖已收到的新版本。
type ScreeningRecordStore interface {
	Save(context.Context, ScreeningScope, ScreeningSnapshot) (ScreeningRecord, error)
	List(context.Context, ScreeningScope, ScreeningRecordQuery) (ScreeningRecordList, error)
}

// screeningInput 返回可读的固定业务输入错误，message不包含上游密钥或SQL。
func screeningInput(message string) error {
	return fmt.Errorf("%w：%s", ErrScreeningRecordInput, message)
}

// screeningScopeValid 检查可信范围完整，空租户不能退化成全局查询。
func screeningScopeValid(scope ScreeningScope) bool {
	return scope.TenantID != "" && scope.PositionID != "" && scope.UserEmail != ""
}

// screeningNumberValid 允许0到100的有限分数，nil表示没有该项数值。
func screeningNumberValid(value *float64) bool {
	return value == nil || !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 100
}

// screeningUTC 创建规范UTC时间副本，避免修改调用方指针并让等价时区重传具有相同指纹。
func screeningUTC(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

// validateScreeningRound 验证状态和真实结果的对应关系；崩溃恢复的失败或取消允许未知结束时间与耗时。
func validateScreeningRound(round *ScreeningRoundSnapshot) error {
	if round.Kind != "ai" && round.Kind != "keyword" && round.Kind != "none" {
		return screeningInput("筛选方式只能是AI、关键词或未执行")
	}
	if utf8.RuneCountInString(round.Reason) > 16000 || !screeningNumberValid(round.Score) || !screeningNumberValid(round.Threshold) {
		return screeningInput("每轮原因最多16000字，分数与阈值应为0到100的有效数字")
	}
	round.StartedAt = screeningUTC(round.StartedAt)
	round.FinishedAt = screeningUTC(round.FinishedAt)
	if round.StartedAt != nil && round.StartedAt.IsZero() || round.FinishedAt != nil && round.FinishedAt.IsZero() {
		return screeningInput("筛选时间不能是空时间")
	}
	if round.DurationMS != nil && *round.DurationMS < 0 {
		return screeningInput("筛选耗时不能为负数")
	}
	if round.FinishedAt != nil && (round.StartedAt == nil || round.FinishedAt.Before(*round.StartedAt)) {
		return screeningInput("筛选完成时间不能早于开始时间")
	}
	if (round.FinishedAt == nil) != (round.DurationMS == nil) {
		return screeningInput("完成时间与耗时请同时提供，未知时同时留空")
	}
	if round.Kind == "keyword" && (round.Score != nil || round.Threshold != nil) {
		return screeningInput("关键词判断没有AI分数，请保留空值")
	}
	switch round.Status {
	case "not_executed":
		if round.Kind != "none" || round.Accepted != nil || round.Score != nil || round.Threshold != nil || round.StartedAt != nil || round.FinishedAt != nil || round.DurationMS != nil {
			return screeningInput("未执行轮次不能填写结果、分数或时间")
		}
	case "running":
		if round.Kind == "none" || round.StartedAt == nil || round.FinishedAt != nil || round.DurationMS != nil || round.Accepted != nil || round.Score != nil {
			return screeningInput("运行中请只保留真实开始时间，结果暂时留空")
		}
	case "passed", "rejected":
		if round.Kind == "none" || round.Accepted == nil || round.StartedAt == nil || round.FinishedAt == nil || round.DurationMS == nil || round.Kind == "ai" && round.Score == nil {
			return screeningInput("已完成判断需要真实结果、开始完成时间和耗时，AI判断还需要分数")
		}
		if *round.Accepted != (round.Status == "passed") {
			return screeningInput("通过状态与判断结果不一致")
		}
	case "failed", "cancelled":
		if round.Accepted != nil || round.Score != nil || round.Threshold != nil || strings.TrimSpace(round.Reason) == "" {
			return screeningInput("失败或取消请保留原因，不填写未获得的分数和结果")
		}
	default:
		return screeningInput("筛选轮次状态没认出来")
	}
	return nil
}

// validateScreeningSnapshot 校验并规范完整快照；身份字段用于防止同一attempt被换绑到其他任务或候选人。
func validateScreeningSnapshot(snapshot *ScreeningSnapshot) error {
	snapshot.AttemptID = strings.ToLower(strings.TrimSpace(snapshot.AttemptID))
	snapshot.PlatformID = strings.ToLower(strings.TrimSpace(snapshot.PlatformID))
	if !screeningUUIDPattern.MatchString(snapshot.AttemptID) || snapshot.AttemptID == "00000000-0000-0000-0000-000000000000" {
		return screeningInput("请提供本次筛选的有效UUID编号")
	}
	if strings.TrimSpace(snapshot.TaskID) == "" || len(snapshot.TaskID) > 200 || snapshot.PlatformID == "" || len(snapshot.PlatformID) > 80 || strings.TrimSpace(snapshot.CandidateKey) == "" || len(snapshot.CandidateKey) > 2048 || len(snapshot.PlatformCandidateID) > 1000 || utf8.RuneCountInString(snapshot.CandidateName) > 500 {
		return screeningInput("任务、平台或候选人标识不完整或过长")
	}
	if snapshot.Revision < 1 || snapshot.OccurredAt.IsZero() || utf8.RuneCountInString(snapshot.TerminationReason) > 16000 {
		return screeningInput("快照版本、筛选时间或结束原因不符合要求")
	}
	snapshot.OccurredAt = snapshot.OccurredAt.UTC()
	if err := validateScreeningRound(&snapshot.First); err != nil {
		return err
	}
	if err := validateScreeningRound(&snapshot.Second); err != nil {
		return err
	}
	if snapshot.Completed && (snapshot.First.Status == "running" || snapshot.Second.Status == "running") {
		return screeningInput("已结束的筛选不能还有运行中的轮次")
	}
	return nil
}

// screeningSnapshotHash 计算规范JSON的内容摘要，包含revision与全部可写字段。
func screeningSnapshotHash(snapshot ScreeningSnapshot) (string, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// screeningIdentityMatches 校验原始任务、平台、候选人指纹和开始时间，平台候选人编号填写后也不能更换。
func screeningIdentityMatches(old, incoming ScreeningSnapshot) bool {
	return old.AttemptID == incoming.AttemptID && old.TaskID == incoming.TaskID && old.PlatformID == incoming.PlatformID && old.CandidateKey == incoming.CandidateKey && old.OccurredAt.Equal(incoming.OccurredAt) && (old.PlatformCandidateID == "" || old.PlatformCandidateID == incoming.PlatformCandidateID || incoming.Revision < old.Revision && incoming.PlatformCandidateID == "")
}

// newScreeningRecordID 生成独立UUID作为记录主键，避免不同岗位误用相同attempt时碰撞全局主键。
func newScreeningRecordID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	text := hex.EncodeToString(raw[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}

// validateScreeningQuery 限定安全分页及各轮范围，不将零分当成未设置。
func validateScreeningQuery(query *ScreeningRecordQuery) error {
	if query.Page == 0 {
		query.Page = 1
	}
	if query.Page < 1 || query.Page > int(^uint(0)>>1)/screeningRecordPageSize {
		return screeningInput("页码超出可用范围")
	}
	for _, pair := range [][2]*float64{{query.FirstScoreMin, query.FirstScoreMax}, {query.SecondScoreMin, query.SecondScoreMax}} {
		if !screeningNumberValid(pair[0]) || !screeningNumberValid(pair[1]) || pair[0] != nil && pair[1] != nil && *pair[0] > *pair[1] {
			return screeningInput("分数范围应在0到100之间，最低不能高于最高")
		}
	}
	for _, pair := range [][2]*time.Time{{query.FirstStartedFrom, query.FirstStartedTo}, {query.SecondStartedFrom, query.SecondStartedTo}} {
		if pair[0] != nil && pair[0].IsZero() || pair[1] != nil && pair[1].IsZero() || pair[0] != nil && pair[1] != nil && pair[0].After(*pair[1]) {
			return screeningInput("开始时间范围不正确")
		}
	}
	return nil
}
