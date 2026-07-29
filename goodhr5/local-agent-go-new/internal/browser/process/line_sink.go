// Package process 文件作用：把 Browser Worker stdout 的分段字节整理成完整日志行。
package process

import (
	"bytes"
	"sync"
)

// lineSinkWriter 缓存不完整行，并把完整 JSON 行交给统一日志入口。
type lineSinkWriter struct {
	mu      sync.Mutex
	pending []byte
	sink    func([]byte)
}

// Write 接收进程输出分片并逐行回调。
func (w *lineSinkWriter) Write(content []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, content...)
	for {
		index := bytes.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		line := bytes.TrimSpace(w.pending[:index])
		if len(line) > 0 && w.sink != nil {
			w.sink(append([]byte(nil), line...))
		}
		w.pending = append(w.pending[:0], w.pending[index+1:]...)
	}
	return len(content), nil
}

// Flush 把进程退出时尚未换行的最后一条日志交给统一入口。
func (w *lineSinkWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	line := bytes.TrimSpace(w.pending)
	if len(line) > 0 && w.sink != nil {
		w.sink(append([]byte(nil), line...))
	}
	w.pending = nil
}
