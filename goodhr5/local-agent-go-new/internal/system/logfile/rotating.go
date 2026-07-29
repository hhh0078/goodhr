// Package logfile 文件作用：提供本地进程日志按大小轮转能力，避免日志文件无限增长。
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Writer 把日志写入当前文件，并在达到上限时保留有限数量的历史文件。
type Writer struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

// Open 打开一个按大小轮转的日志写入器。
func Open(path string, maxBytes int64, maxBackups int) (*Writer, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("日志文件大小上限必须大于 0")
	}
	if maxBackups < 0 {
		return nil, fmt.Errorf("日志备份数量不能小于 0")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败：%w", err)
	}
	writer := &Writer{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
	if err := writer.openCurrent(); err != nil {
		return nil, err
	}
	return writer, nil
}

// Write 写入日志，并在本次写入会超过上限时先执行轮转。
func (w *Writer) Write(content []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, fmt.Errorf("日志文件已经关闭")
	}
	if w.size > 0 && w.size+int64(len(content)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := w.file.Write(content)
	w.size += int64(written)
	return written, err
}

// Close 关闭当前日志文件。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// openCurrent 打开当前日志文件并读取已有大小。
func (w *Writer) openCurrent() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败：%w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("读取日志文件状态失败：%w", err)
	}
	w.file = file
	w.size = info.Size()
	return nil
}

// rotate 关闭当前文件并把历史文件依次向后移动。
func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("关闭待轮转日志失败：%w", err)
	}
	w.file = nil
	if w.maxBackups > 0 {
		_ = os.Remove(w.backupPath(w.maxBackups))
		for index := w.maxBackups - 1; index >= 1; index-- {
			source := w.backupPath(index)
			target := w.backupPath(index + 1)
			if _, err := os.Stat(source); err == nil {
				if err := os.Rename(source, target); err != nil {
					return fmt.Errorf("移动历史日志失败：%w", err)
				}
			}
		}
		if _, err := os.Stat(w.path); err == nil {
			if err := os.Rename(w.path, w.backupPath(1)); err != nil {
				return fmt.Errorf("轮转当前日志失败：%w", err)
			}
		}
	} else {
		_ = os.Remove(w.path)
	}
	return w.openCurrent()
}

// backupPath 返回指定序号的日志备份路径。
func (w *Writer) backupPath(index int) string {
	return w.path + "." + strconv.Itoa(index)
}
