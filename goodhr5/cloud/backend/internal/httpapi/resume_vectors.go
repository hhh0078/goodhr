// 本文件提供简历向量化数量、立即返回的后台任务及断点恢复，付款调用与HTTP连接分离，已成功的分片不会因搜索故障重算。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ResumeVectorService 协调当前权限、持久向量回执、全局限流任务及搜索副本。
type ResumeVectorService struct {
	candidates *CandidateService
	wallet     *AIWalletService
	store      resumeVectorStore
	tasks      resumeVectorTaskStore
	search     resumeSearchIndex
	persistent bool
	once       sync.Once
	embed      func(context.Context, string, []string) ([][]float64, error)
}

// NewResumeVectorService 复用候选人认证与Redis，stores允许助手与设置页面使用同一向量存储。
func NewResumeVectorService(candidates *CandidateService, wallet *AIWalletService, db *sql.DB, search resumeSearchIndex, stores ...resumeVectorStore) *ResumeVectorService {
	service := &ResumeVectorService{candidates: candidates, wallet: wallet, search: search, persistent: db != nil}
	if candidates != nil {
		service.store = newResumeVectorStore(db, candidates.store)
		if candidates.auth != nil {
			service.tasks = newResumeVectorTaskStore(candidates.auth.store)
		}
	}
	if len(stores) > 0 && stores[0] != nil {
		service.store = stores[0]
	}
	if wallet != nil {
		service.embed = wallet.EmbedForUser
	}
	return service
}

// eligibility 校验本人同站点内置AI、余额、搜索配置与生产任务持久化，不通过时禁止创建付费任务。
func (s *ResumeVectorService) eligibility(scope CandidateLibraryScope) (bool, string) {
	if s.tasks == nil || s.store == nil || s.wallet == nil || s.wallet.aiConfigs == nil || s.wallet.wallet == nil {
		return false, "向量化服务还没准备好，请稍后再试"
	}
	if s.persistent {
		if _, ok := s.tasks.(*redisResumeVectorTasks); !ok {
			return false, "后台任务缓存尚未配置，暂不启动付费向量化"
		}
	}
	if s.search == nil || !s.search.Ready() {
		return false, "高级搜索服务尚未配置，请联系管理员接好搜索服务"
	}
	config, err := s.wallet.aiConfigs.UserConfig(scope.UserEmail)
	if err != nil || !config.Enabled || !isCurrentSiteAIURL(config.BaseURL) || strings.TrimSpace(config.APIKey) == "" {
		return false, "请先启用当前GoodHR站点的内置AI配置"
	}
	owner, err := s.wallet.wallet.UserEmailByAIKey(config.APIKey)
	if err != nil || !strings.EqualFold(owner, scope.UserEmail) {
		return false, "内置AI Key与当前账号没有核对上，请检查个人配置"
	}
	balance, err := s.wallet.wallet.BalanceUnits(scope.UserEmail)
	if err != nil {
		return false, "AI余额暂时没查清楚，请稍后再试"
	}
	if balance <= 0 {
		return false, "AI余额不足，充值后再生成向量"
	}
	upstream, err := s.wallet.loadBuiltinAIConfig()
	if err != nil || strings.TrimSpace(upstream.UpstreamAPIKey) == "" {
		return false, "内置向量服务还没配置完整"
	}
	if _, err = normalizeAIEmbeddingsURL(upstream.UpstreamBaseURL); err != nil {
		return false, "内置向量服务地址还没配置完整"
	}
	return true, ""
}

// Route 返回真实简历数量或立即创建后台任务；浏览器关闭、刷新或断开不会取消已确认的任务。
func (s *ResumeVectorService) Route(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !aiSearchEnabled() {
		writeError(w, 503, "简历向量搜索暂未启用")
		return
	}
	if s == nil || s.candidates == nil {
		writeError(w, 503, "向量化服务还没准备好")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, 405, "这个入口只支持查看和启动向量化")
		return
	}
	scope, ok := s.candidates.libraryScope(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, 503, "向量回执存储尚未准备好")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	total, indexed, err := s.store.Counts(ctx, scope)
	if err != nil {
		writeError(w, 503, "向量化数量暂时没读清楚，请稍后再试")
		return
	}
	eligible, reason := s.eligibility(scope)
	task := resumeVectorTask{}
	if s.tasks != nil {
		current, getErr := s.tasks.Get(ctx, scope.UserEmail)
		if getErr != nil && !errors.Is(getErr, ErrNotFound) {
			writeError(w, 503, "后台任务状态暂时没读到，请稍后核对")
			return
		}
		if getErr == nil && current.TenantID == scope.TenantID {
			task = current
		}
	}
	if r.Method == http.MethodPost {
		if !eligible {
			writeError(w, http.StatusConflict, reason)
			return
		}
		if total <= indexed {
			writeError(w, http.StatusConflict, "当前可见简历已经全部向量化啦")
			return
		}
		now := time.Now()
		created := resumeVectorTask{ID: vectorTaskID(), Owner: scope.UserEmail, TenantID: scope.TenantID, Status: "queued", Stage: "ready", Message: "已进入后台队列，可以关闭弹框", CreatedAt: now, ExpiresAt: now.Add(resumeVectorTaskTTL)}
		task, err = s.tasks.Reserve(ctx, created)
		if err != nil {
			writeError(w, 503, "任务没有确认收到，请稍后核对再试")
			return
		}
		if task.TenantID != scope.TenantID {
			writeError(w, 409, "账号团队刚发生变化，等上一项任务结束后再试")
			return
		}
		go s.run(scope.UserEmail)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "task_id": task.ID, "status": task.Status})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "total": total, "indexed": indexed, "pending": max(0, total-indexed), "running": vectorTaskActive(task.Status), "task_id": task.ID, "status": task.Status, "processed": task.Processed, "failed": task.Failed, "message": task.Message, "eligible": eligible, "reason": reason, "model": builtinEmbeddingModel, "input_price_per_million": 0.125, "output_price_per_million": 0})
}

// StartRecovery 启动独立恢复循环，服务重启后领取未结束任务；已开始但无付费回执的批次不会盲目重算。
func (s *ResumeVectorService) StartRecovery(ctx context.Context) {
	if !aiSearchEnabled() || s == nil || s.tasks == nil {
		return
	}
	s.once.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				items, err := s.tasks.Pending(callCtx)
				cancel()
				if err == nil {
					for _, item := range items {
						go s.run(item.Owner)
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

// currentScope 每批调用及写搜索前重新读取团队和角色，撤权后不得沿用创建任务时的管理员身份。
func (s *ResumeVectorService) currentScope(task resumeVectorTask) (CandidateLibraryScope, error) {
	if s.candidates == nil || s.candidates.tenantStore == nil {
		return CandidateLibraryScope{}, ErrNotFound
	}
	tenant, err := s.candidates.tenantStore.GetOrCreateTenant(task.Owner)
	if err != nil || tenant.ID != task.TenantID {
		return CandidateLibraryScope{}, ErrNotFound
	}
	admin, err := s.candidates.tenantStore.IsTenantAdmin(tenant.ID, task.Owner)
	if err != nil {
		return CandidateLibraryScope{}, err
	}
	return CandidateLibraryScope{TenantID: tenant.ID, UserEmail: task.Owner, IsAdmin: admin}, nil
}

// checkpoint 在后续外部调用前持久确认新断点，租约令牌或版本变化即停止旧执行者。
func (s *ResumeVectorService) checkpoint(task *resumeVectorTask) error {
	expected := task.Version
	task.Version++
	task.LeaseUntil = time.Now().Add(resumeVectorLease).UnixMilli()
	if !vectorTaskActive(task.Status) {
		task.LeaseUntil = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ok, err := s.tasks.Save(ctx, *task, expected)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("任务租约已变化，旧执行已经停止")
	}
	return nil
}

// finish 保存终态并释放租约，提示中不含候选人身份或原始模型报错。
func (s *ResumeVectorService) finish(task *resumeVectorTask, status, message string) {
	task.Status = status
	task.Message = message
	task.Stage = "done"
	task.CandidateID = ""
	task.BatchIDs = nil
	_ = s.checkpoint(task)
}

// interruptedBatchSaved 确认中断批次全部向量已经写入持久回执，只有确定保存的结果才能继续索引。
func (s *ResumeVectorService) interruptedBatchSaved(ctx context.Context, scope CandidateLibraryScope, task resumeVectorTask) bool {
	if task.CandidateID == "" || len(task.BatchIDs) == 0 {
		return false
	}
	item, err := s.store.Get(ctx, scope, task.CandidateID)
	if err != nil {
		return false
	}
	valid := map[string]bool{}
	for _, chunk := range item.Chunks {
		if len(chunk.Vector) == 1024 {
			valid[chunk.ID] = true
		}
	}
	for _, id := range task.BatchIDs {
		if !valid[id] {
			return false
		}
	}
	return true
}

// run 领取全局租约后有界枚举待处理简历，与原HTTP请求无关；每份简历均保存分片回执。
func (s *ResumeVectorService) run(owner string) {
	claimCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	task, claimed, err := s.tasks.Claim(claimCtx, owner)
	cancel()
	if err != nil || !claimed {
		return
	}
	ctx, stop := context.WithDeadline(context.Background(), task.ExpiresAt)
	defer stop()
	scope, err := s.currentScope(task)
	if err != nil {
		s.finish(&task, "failed", "账号团队或权限已经变化，任务已停止")
		return
	}
	if task.Stage == "embedding" && !s.interruptedBatchSaved(ctx, scope, task) {
		s.finish(&task, "uncertain", "上次向量调用结果未确认，可能已有上游费用；请核对后再明确重试")
		return
	}
	task.Stage = "ready"
	task.BatchIDs = nil
	if err = s.checkpoint(&task); err != nil {
		return
	}
	for ctx.Err() == nil {
		scope, err = s.currentScope(task)
		if err != nil {
			s.finish(&task, "failed", "账号团队或权限已经变化，任务已停止")
			return
		}
		if eligible, reason := s.eligibility(scope); !eligible {
			s.finish(&task, "failed", reason)
			return
		}
		ids, listErr := s.store.Pending(ctx, scope, 1)
		if listErr != nil {
			s.finish(&task, "failed", "待处理简历暂时没读到，已完成的向量仍然保留")
			return
		}
		if len(ids) == 0 {
			s.finish(&task, "completed", "当前可见简历已全部向量化")
			return
		}
		err = s.processCandidate(ctx, scope, ids[0], &task)
		if err != nil {
			task.Failed++
			if task.Stage == "embedding" {
				s.finish(&task, "uncertain", "本批向量调用或保存回执未确认，请先核对；已经保存的分片不会重复计算")
			} else {
				s.finish(&task, "failed", "向量化暂时停下了，已经保存的分片会在下次任务中继续使用；请检查搜索服务、余额或简历更新后再试")
			}
			return
		}
		task.Processed++
		task.Message = "正在后台整理简历，已完成的分片都记好了"
		task.CandidateID = ""
		task.BatchIDs = nil
		task.Stage = "ready"
		if err = s.checkpoint(&task); err != nil {
			return
		}
	}
	s.finish(&task, "failed", "任务已超过保留时间，已完成的简历向量仍保留")
}

// processCandidate 按字段稳定复用成功分片，模型与搜索分别保存断点，搜索失败不再次调用模型。
func (s *ResumeVectorService) processCandidate(ctx context.Context, scope CandidateLibraryScope, id string, task *resumeVectorTask) error {
	candidate, err := s.candidates.store.GetPositionCandidate(scope.TenantID, id, "", scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return err
	}
	if candidate.RowVersion <= 0 {
		return errors.New("简历缺少可靠行版本")
	}
	chunks := buildResumeVectorChunks(candidate)
	previous, getErr := s.store.Get(ctx, scope, id)
	if getErr != nil && !errors.Is(getErr, ErrNotFound) {
		return getErr
	}
	if getErr == nil && previous.ResumeVersion == candidate.RowVersion && previous.Profile == resumeVectorProfile {
		known := map[string][]float64{}
		for _, chunk := range previous.Chunks {
			if len(chunk.Vector) == 1024 {
				known[chunk.ID] = chunk.Vector
			}
		}
		for i := range chunks {
			chunks[i].Vector = known[chunks[i].ID]
		}
	}
	record := resumeVectorRecord{CandidateID: id, TenantID: scope.TenantID, OwnerEmail: candidate.UserEmail, ResumeVersion: candidate.RowVersion, Profile: resumeVectorProfile, Chunks: chunks, Facts: buildResumeVectorFacts(candidate), UpdatedAt: time.Now()}
	if err = s.store.Save(ctx, scope, record); err != nil {
		return err
	}
	for {
		indices := []int{}
		texts := []string{}
		for i, chunk := range record.Chunks {
			if len(chunk.Vector) == 0 {
				indices = append(indices, i)
				texts = append(texts, chunk.Text)
				if len(indices) == maxEmbeddingBatch {
					break
				}
			}
		}
		if len(indices) == 0 {
			break
		}
		currentScope, scopeErr := s.currentScope(*task)
		if scopeErr != nil {
			return scopeErr
		}
		current, readErr := s.candidates.store.GetPositionCandidate(currentScope.TenantID, id, "", currentScope.UserEmail, currentScope.IsAdmin)
		if readErr != nil {
			return readErr
		}
		if current.RowVersion != record.ResumeVersion {
			return ErrCandidateVersion
		}
		task.CandidateID = id
		task.Stage = "embedding"
		task.BatchIDs = nil
		for _, index := range indices {
			task.BatchIDs = append(task.BatchIDs, record.Chunks[index].ID)
		}
		if err = s.checkpoint(task); err != nil {
			return err
		}
		vectors, callErr := s.embed(ctx, scope.UserEmail, texts)
		if callErr != nil {
			return callErr
		}
		if len(vectors) != len(indices) {
			return errors.New("向量回执数量不完整")
		}
		currentScope, scopeErr = s.currentScope(*task)
		if scopeErr != nil {
			return scopeErr
		}
		current, readErr = s.candidates.store.GetPositionCandidate(currentScope.TenantID, id, "", currentScope.UserEmail, currentScope.IsAdmin)
		if readErr != nil {
			return readErr
		}
		if current.RowVersion != record.ResumeVersion {
			return ErrCandidateVersion
		}
		for i, index := range indices {
			if len(vectors[i]) != 1024 {
				return errors.New("向量维度不匹配")
			}
			record.Chunks[index].Vector = vectors[i]
		}
		record.UpdatedAt = time.Now()
		if err = s.store.Save(ctx, currentScope, record); err != nil {
			return err
		}
		task.Stage = "ready"
		task.BatchIDs = nil
		if err = s.checkpoint(task); err != nil {
			return err
		}
	}
	currentScope, err := s.currentScope(*task)
	if err != nil {
		return err
	}
	current, err := s.candidates.store.GetPositionCandidate(currentScope.TenantID, id, "", currentScope.UserEmail, currentScope.IsAdmin)
	if err != nil {
		return err
	}
	if current.RowVersion != record.ResumeVersion {
		return ErrCandidateVersion
	}
	task.Stage = "indexing"
	task.CandidateID = id
	if err = s.checkpoint(task); err != nil {
		return err
	}
	if err = s.search.Index(ctx, record); err != nil {
		return err
	}
	record.Complete = true
	record.UpdatedAt = time.Now()
	return s.store.Save(ctx, currentScope, record)
}
