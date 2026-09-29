// Package diagnostics 文件作用：浏览器进程消失时同步落盘异常退出日志，并在网络恢复后上传。
package diagnostics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// BrowserEventUploader 是浏览器异常日志的云端上传接口。
type BrowserEventUploader interface {
	UploadBrowserEvents(context.Context, string, []storage.BrowserEvent) error
}

// BrowserIncidentRecorder 保存无岗位时也能观察到的浏览器异常退出事件。
type BrowserIncidentRecorder struct {
	store    *storage.Store
	cloud    BrowserEventUploader
	mu       sync.RWMutex
	uploadMu sync.Mutex
	ownerID  string
	token    string
}

// NewBrowserIncidentRecorder 创建浏览器异常退出记录器。
func NewBrowserIncidentRecorder(store *storage.Store, cloud BrowserEventUploader) *BrowserIncidentRecorder {
	return &BrowserIncidentRecorder{store: store, cloud: cloud}
}

// Bind 记录已验证的账号并触发等待上传的异常日志补传。
func (r *BrowserIncidentRecorder) Bind(ownerID, token string) {
	if r == nil || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(token) == "" {
		return
	}
	r.mu.Lock()
	r.ownerID, r.token = ownerID, token
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := r.store.ClaimUnownedBrowserEvents(ctx, ownerID); err != nil {
		log.Printf("关联浏览器退出日志失败：%v", err)
	}
	cancel()
	go r.Flush(context.Background())
}

// WorkerLine 只提取浏览器或 Worker 意外退出日志，先同步写入 SQLite 再异步上传。
func (r *BrowserIncidentRecorder) WorkerLine(line []byte) {
	if r == nil || r.store == nil {
		return
	}
	var item struct {
		Action    string `json:"action"`
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(line, &item) != nil {
		return
	}
	if item.Action != "browser.exit" && item.Action != "browser.crash" && item.Action != "browser.process_exit" &&
		item.Action != "worker.crash" && item.Action != "worker.exit" && item.Action != "ocr.exit" && item.Action != "agent.previous_exit" &&
		item.Action != "agent.start" && item.Action != "agent.exit" &&
		!((item.Action == "browser.start" || item.Action == "browser.stop") && (item.Status == "success" || item.Status == "failed")) {
		return
	}
	line = normalizeIncident(line)
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		log.Printf("创建浏览器退出日志编号失败：%v", err)
		return
	}
	r.mu.RLock()
	ownerID := r.ownerID
	r.mu.RUnlock()
	createdAt, err := time.Parse(time.RFC3339Nano, item.Timestamp)
	if err != nil {
		createdAt = time.Now().UTC()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event := storage.BrowserEvent{
		EventID: hex.EncodeToString(bytes[:]), OwnerID: ownerID,
		PositionID: r.store.RunningPositionID(ctx), Payload: append(json.RawMessage(nil), line...), CreatedAt: createdAt,
	}
	if err := r.store.SaveBrowserEvent(ctx, event); err != nil {
		log.Printf("保存浏览器异常退出日志失败：%v", err)
		return
	}
	go r.Flush(context.Background())
}

// RecordLifecycle 把本地程序启动、正常退出和上次异常退出保存到相同上传队列。
func (r *BrowserIncidentRecorder) RecordLifecycle(action, message string, abnormal bool) {
	r.RecordLifecycleDetails(action, message, abnormal, nil)
}

// RecordLifecycleDetails 给主程序生命周期附加调用栈、退出步骤和上次运行证据。
func (r *BrowserIncidentRecorder) RecordLifecycleDetails(action, message string, abnormal bool, details map[string]any) {
	category, status := "运行", "success"
	if abnormal {
		category, status = "异常退出", "failed"
	}
	item := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": category,
		"action": action, "step": "process_lifecycle", "status": status,
		"is_error": abnormal, "error_message": "", "message": message,
		"input": map[string]any{}, "output": map[string]any{"reason": message}, "diagnostics": details,
	}
	if abnormal {
		item["error_message"] = message
	}
	encoded, _ := json.Marshal(item)
	r.WorkerLine(Normalize(encoded))
}

// Flush 按当前账号批量补传已持久保存的异常事件，失败时保留原记录。
func (r *BrowserIncidentRecorder) Flush(ctx context.Context) {
	if r == nil || r.cloud == nil {
		return
	}
	r.uploadMu.Lock()
	defer r.uploadMu.Unlock()
	r.mu.RLock()
	ownerID, token := r.ownerID, r.token
	r.mu.RUnlock()
	if ownerID == "" || token == "" {
		return
	}
	claimCtx, cancelClaim := context.WithTimeout(ctx, 3*time.Second)
	if err := r.store.ClaimUnownedBrowserEvents(claimCtx, ownerID); err != nil {
		log.Printf("关联待传浏览器日志失败：%v", err)
	}
	cancelClaim()
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		items, err := r.store.PendingBrowserEvents(batchCtx, ownerID)
		if err == nil && len(items) > 0 {
			for index := range items {
				items[index].Payload = normalizeIncident(items[index].Payload)
			}
			err = r.cloud.UploadBrowserEvents(batchCtx, token, items)
		}
		if err == nil && len(items) > 0 {
			ids := make([]string, 0, len(items))
			for _, item := range items {
				ids = append(ids, item.EventID)
			}
			err = r.store.DeleteBrowserEvents(batchCtx, ids)
		}
		cancel()
		if err != nil {
			log.Printf("浏览器异常退出日志暂未传到云端，稍后重试：%v", err)
			return
		}
		if len(items) < 200 {
			return
		}
	}
}

// Run 每三十秒尝试补传异常日志，直到本地程序退出。
func (r *BrowserIncidentRecorder) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Flush(ctx)
		}
	}
}
