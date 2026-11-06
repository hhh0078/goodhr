// Package diagnostics 文件作用：将本地程序、浏览器和 OCR 诊断自动上传，不写本地日志文件或离线日志表。
package diagnostics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// BrowserEventUploader 是运行诊断的云端上传接口。
type BrowserEventUploader interface {
	UploadBrowserEvents(context.Context, string, []storage.BrowserEvent) error
}

// LogUploadStatus 提供可观察的上传结果，失败不能伪装成已经上传。
type LogUploadStatus struct {
	Pending   int    `json:"pending"`
	LastError string `json:"last_error,omitempty"`
	Dropped   int    `json:"dropped"`
}

// BrowserIncidentRecorder 复用运行日志接口，只在内存中等待批量上传。
type BrowserIncidentRecorder struct {
	store             *storage.Store
	cloud             BrowserEventUploader
	mu                sync.Mutex
	uploadMu          sync.Mutex
	ownerID           string
	token             string
	pending           []storage.BrowserEvent
	pendingBytes      int
	lastError         string
	dropped           int
	reportedDropped   int
	wake              chan struct{}
	positionID        string
	positionCheckedAt time.Time
}

// NewBrowserIncidentRecorder 创建在线诊断上传器；SQLite 只用于查询当前岗位，不保存日志队列。
func NewBrowserIncidentRecorder(store *storage.Store, cloud BrowserEventUploader) *BrowserIncidentRecorder {
	return &BrowserIncidentRecorder{store: store, cloud: cloud, wake: make(chan struct{}, 1)}
}

// Bind 接收已验证的账号，启动日志只归属首次绑定账号；退出时立即停用凭证。
func (r *BrowserIncidentRecorder) Bind(ownerID, token string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(token) == "" {
		r.token = ""
	} else {
		r.ownerID, r.token = ownerID, token
		for i := range r.pending {
			if r.pending[i].OwnerID == "" {
				r.pending[i].OwnerID = ownerID
			}
		}
	}
	r.mu.Unlock()
	r.notify()
}

// WorkerLine 接收每条运行诊断，保留原始动作与错误证据，并限制内存占用。
func (r *BrowserIncidentRecorder) WorkerLine(line []byte) {
	if r == nil {
		return
	}
	var item map[string]any
	if json.Unmarshal(normalizeIncident(Normalize(line)), &item) != nil {
		return
	}
	// 统一接收运行诊断；原动作另外保留，不把普通超时冒充浏览器崩溃。
	if action, ok := item["action"].(string); ok && action != "" {
		item["source_action"] = action
	} else {
		item["source_action"] = "agent.log"
	}
	item["action"] = "runtime.log"
	safe := UploadSafeDiagnostic(item)
	line, _ = json.Marshal(safe)
	line = normalizeIncident(line)
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(item["timestamp"]))
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	positionID := r.currentPosition()
	r.mu.Lock()
	event := storage.BrowserEvent{EventID: hex.EncodeToString(id[:]), OwnerID: r.ownerID, PositionID: positionID, Payload: line, CreatedAt: createdAt}
	r.pending = append(r.pending, event)
	r.pendingBytes += len(line)
	// 只保留最多 4 MiB 的在线在途日志；优先移除普通记录，并公开丢弃数量。
	for len(r.pending) > 2000 || r.pendingBytes > 4<<20 {
		index := 0
		for i, e := range r.pending {
			if !strings.Contains(string(e.Payload), `"is_error":true`) {
				index = i
				break
			}
		}
		r.pendingBytes -= len(r.pending[index].Payload)
		r.pending = append(r.pending[:index], r.pending[index+1:]...)
		r.dropped++
	}
	r.mu.Unlock()
	if item["is_error"] == true {
		r.notify()
	}
}

// notify 合并上传唤醒，避免每条字段日志都创建一个上传协程。
func (r *BrowserIncidentRecorder) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Status 返回内存中等待上传的数量和最近失败原因。
func (r *BrowserIncidentRecorder) Status() LogUploadStatus {
	if r == nil {
		return LogUploadStatus{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return LogUploadStatus{Pending: len(r.pending), LastError: r.lastError, Dropped: r.dropped}
}

// RecordLifecycle 把启动、正常退出和可捕获崩溃送入同一上传通道。
func (r *BrowserIncidentRecorder) RecordLifecycle(action, message string, abnormal bool) {
	r.RecordLifecycleDetails(action, message, abnormal, nil)
}

// RecordLifecycleDetails 保留退出步骤、调用栈和耗时，不依赖用户读取本地文件。
func (r *BrowserIncidentRecorder) RecordLifecycleDetails(action, message string, abnormal bool, details map[string]any) {
	category, status := "运行", "success"
	if abnormal {
		category, status = "异常退出", "failed"
	}
	item := map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": category, "action": action, "step": "process_lifecycle", "status": status, "is_error": abnormal, "message": message, "diagnostics": details}
	if abnormal {
		item["error_message"] = message
	}
	line, _ := json.Marshal(item)
	r.WorkerLine(line)
}

// Flush 在线批量上传，只有云端确认后才释放内存；上传本身的错误不得递归写回日志器。
func (r *BrowserIncidentRecorder) Flush(ctx context.Context) {
	if r == nil || r.cloud == nil {
		return
	}
	r.uploadMu.Lock()
	defer r.uploadMu.Unlock()
	for ctx.Err() == nil {
		r.mu.Lock()
		owner, token := r.ownerID, r.token
		batch := make([]storage.BrowserEvent, 0, 200)
		dropped := r.dropped
		if dropped > r.reportedDropped && owner != "" {
			batch = append(batch, overflowEvent(owner, dropped))
		}
		for _, e := range r.pending {
			if e.OwnerID == owner && owner != "" && len(batch) < 200 {
				batch = append(batch, e)
			}
		}
		r.mu.Unlock()
		if token == "" || len(batch) == 0 {
			return
		}
		uploadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := r.cloud.UploadBrowserEvents(uploadCtx, token, batch)
		cancel()
		r.mu.Lock()
		if err != nil {
			r.lastError = diagnosticText(string(Redact([]byte(err.Error()))), 500)
			r.mu.Unlock()
			_, _ = fmt.Fprintln(os.Stderr, "运行日志尚未上传成功：", diagnosticText(string(Redact([]byte(err.Error()))), 500))
			return
		}
		ids := make(map[string]bool, len(batch))
		for _, e := range batch {
			ids[e.EventID] = true
		}
		kept := r.pending[:0]
		for _, e := range r.pending {
			if ids[e.EventID] {
				r.pendingBytes -= len(e.Payload)
			} else {
				kept = append(kept, e)
			}
		}
		clear(r.pending[len(kept):])
		r.pending = kept
		r.reportedDropped = dropped
		r.lastError = ""
		r.mu.Unlock()
	}
}

// Run 每秒上传普通运行日志，错误到达时立即唤醒，整个进程只运行一个上传循环。
func (r *BrowserIncidentRecorder) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Flush(ctx)
		case <-r.wake:
			r.Flush(ctx)
		}
	}
}

// currentPosition 最多每秒查询一次当前岗位，避免大量日志反过来拖慢任务数据库。
func (r *BrowserIncidentRecorder) currentPosition() string {
	r.mu.Lock()
	refresh := time.Since(r.positionCheckedAt) >= time.Second
	if refresh {
		r.positionCheckedAt = time.Now()
	}
	position := r.positionID
	r.mu.Unlock()
	if refresh && r.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		position = r.store.RunningPositionID(ctx)
		cancel()
		r.mu.Lock()
		r.positionID = position
		r.mu.Unlock()
	}
	return position
}

// overflowEvent 将内存缓冲溢出显式报告到云端，避免只在用户本机留下丢失数量。
func overflowEvent(owner string, dropped int) storage.BrowserEvent {
	var id [16]byte
	_, _ = rand.Read(id[:])
	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]any{"action": "runtime.log", "source_action": "agent.log_upload", "timestamp": now.Format(time.RFC3339Nano), "category": "错误", "is_error": true, "error_code": "LOG_UPLOAD_BUFFER_OVERFLOW", "error_message": "日志上传未及时完成，内存在途缓冲已满", "output": map[string]int{"dropped": dropped}})
	return storage.BrowserEvent{EventID: hex.EncodeToString(id[:]), OwnerID: owner, Payload: payload, CreatedAt: now}
}
