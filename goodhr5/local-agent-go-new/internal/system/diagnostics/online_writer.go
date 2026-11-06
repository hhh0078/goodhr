// Package diagnostics 文件作用：将运行日志交给云端上传通道，不创建用户本地日志文件。
package diagnostics

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// onlineLogBuffer 只在绑定上传入口前短暂保留启动日志，不写入磁盘。
type onlineLogBuffer struct {
	mu    sync.Mutex
	lines [][]byte
	sink  func([]byte)
	flush func(context.Context)
}

// NewOnlineWriter 创建仅向上传通道输出的日志器，启动缓存最多保留两百条。
func NewOnlineWriter() *Writer { return &Writer{online: &onlineLogBuffer{}} }

// SetUploadSink 接入自动上传，并转交初始化期间的日志。
func (w *Writer) SetUploadSink(sink func([]byte), flush func(context.Context)) {
	if w == nil || w.online == nil || sink == nil {
		return
	}
	b := w.online
	b.mu.Lock()
	b.sink, b.flush = sink, flush
	lines := b.lines
	b.lines = nil
	b.mu.Unlock()
	for _, line := range lines {
		sink(line)
	}
}

// write 转交完整脱敏日志；未绑定时只保留有限启动信息。
func (b *onlineLogBuffer) write(data []byte) (int, error) {
	var item map[string]any
	_ = json.Unmarshal(Normalize(data), &item)
	safe, _ := json.Marshal(UploadSafeDiagnostic(item))
	b.mu.Lock()
	sink := b.sink
	if sink == nil {
		if len(safe) > 64<<10 {
			safe = normalizeIncident(safe)
		}
		if len(b.lines) >= 200 {
			b.lines = b.lines[1:]
		}
		b.lines = append(b.lines, safe)
	}
	b.mu.Unlock()
	if sink != nil {
		sink(safe)
	}
	return len(data), nil
}

// close 在正常退出或可恢复崩溃后限时上传最后日志，不依赖本地文件。
func (b *onlineLogBuffer) close() error {
	b.mu.Lock()
	flush := b.flush
	b.mu.Unlock()
	if flush != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		flush(ctx)
	}
	return nil
}
