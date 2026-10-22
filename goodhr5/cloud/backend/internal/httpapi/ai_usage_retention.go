// 本文件提供受共享密钥保护的七日消费清理接口与内存实现，清理仅压缩消费明细，永久保留金额汇总及所有资金凭据。
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const aiUsageRetention = 7 * 24 * time.Hour
const aiUsageCleanupBatch = 5000

// aiUsageDaily 保存已经清理的日金额，不再保存调用模型、用户或Token明细。
type aiUsageDaily struct {
	Day   time.Time
	Units int64
	Count int64
}

// AIUsageCleanupResult 返回单批实际清理数量及金额，HasMore表示调用方应继续调用，金额单位为0.0001元。
type AIUsageCleanupResult struct {
	DeletedRecords int64     `json:"deleted_records"`
	ArchivedUnits  int64     `json:"archived_units"`
	HasMore        bool      `json:"has_more"`
	BlockedCount   int64     `json:"blocked_count"`
	Cutoff         time.Time `json:"cutoff"`
}

// AIUsageRetentionStore 定义只清理消费明细的存储能力，不向清理服务开放余额修改方法。
type AIUsageRetentionStore interface {
	PruneAIUsage(context.Context, time.Time) (AIUsageCleanupResult, error)
}

// AIUsageRetentionService 校验外部定时请求，密钥只从环境与请求头读取，不进入URL、响应或日志。
type AIUsageRetentionService struct {
	store AIUsageRetentionStore
	now   func() time.Time
}

// NewAIUsageRetentionService 复用原钱包存储，未实现清理能力时接口明确不可用。
func NewAIUsageRetentionService(wallet AIWalletStore) *AIUsageRetentionService {
	store, _ := wallet.(AIUsageRetentionStore)
	return &AIUsageRetentionService{store: store, now: time.Now}
}

// Cleanup 供每日定时调用；GET和POST均必须带请求头密钥，不接受URL密钥或浏览器登录态替代授权。
func (s *AIUsageRetentionService) Cleanup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "清理接口只接受 GET 或 POST")
		return
	}
	expected := strings.TrimSpace(os.Getenv("GOODHR_AI_USAGE_CLEANUP_TOKEN"))
	if len(expected) < 32 {
		writeError(w, http.StatusServiceUnavailable, "消费清理密钥尚未配置")
		return
	}
	provided := bearerToken(r.Header.Get("Authorization"))
	if provided == "" {
		provided = r.Header.Get("X-Cleanup-Token")
	}
	wantHash, gotHash := sha256.Sum256([]byte(expected)), sha256.Sum256([]byte(provided))
	if provided == "" || subtle.ConstantTimeCompare(wantHash[:], gotHash[:]) != 1 {
		writeError(w, http.StatusUnauthorized, "消费清理凭据没有核对上")
		return
	}
	if s == nil || s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "消费清理存储暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.store.PruneAIUsage(ctx, s.now().Add(-aiUsageRetention))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "本批消费清理没有完成，账本与原明细已保留，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted_records": result.DeletedRecords, "archived_units": result.ArchivedUnits, "has_more": result.HasMore, "blocked_count": result.BlockedCount, "cutoff": result.Cutoff})
}

// aiUsageDay 将消费时间归入北京时间零点，月边界与现有超管统计完全一致。
func aiUsageDay(at time.Time) time.Time {
	value := at.In(chinaLocation())
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

// PruneAIUsage 在内存锁内先计算全部汇总再交换明细；任何溢出或取消均不更改原记录、余额及充值退款幂等凭据。
func (s *MemoryAIWalletStore) PruneAIUsage(ctx context.Context, cutoff time.Time) (AIUsageCleanupResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := AIUsageCleanupResult{Cutoff: cutoff}
	daily := make(map[string]aiUsageDaily, len(s.usageDaily))
	for key, value := range s.usageDaily {
		daily[key] = value
	}
	retained := make([]AIWalletRecord, 0, len(s.records))
	for _, record := range s.records {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if record.Category != "ai_usage" || record.ChangeUnits >= 0 || !record.CreatedAt.Before(cutoff) {
			retained = append(retained, record)
			continue
		}
		if result.DeletedRecords >= aiUsageCleanupBatch {
			result.HasMore = true
			retained = append(retained, record)
			continue
		}
		if record.ChangeUnits == math.MinInt64 {
			return result, errors.New("消费金额超出可汇总范围")
		}
		day := aiUsageDay(record.CreatedAt)
		key := day.Format("2006-01-02")
		bucket := daily[key]
		bucket.Day = day
		var err error
		bucket.Units, err = checkedAIWalletAdd(bucket.Units, -record.ChangeUnits)
		if err != nil {
			return result, err
		}
		bucket.Count, err = checkedAIWalletAdd(bucket.Count, 1)
		if err != nil {
			return result, err
		}
		result.ArchivedUnits, err = checkedAIWalletAdd(result.ArchivedUnits, -record.ChangeUnits)
		if err != nil {
			return result, err
		}
		result.DeletedRecords++
		daily[key] = bucket
	}
	if s.nextRecordID < int64(len(s.records)) {
		s.nextRecordID = int64(len(s.records))
	}
	s.usageDaily, s.records = daily, retained
	return result, nil
}
