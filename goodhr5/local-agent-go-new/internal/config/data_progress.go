// Package config 文件作用：按真实读写字节报告数据迁移进度，不记录账号、文件名或简历内容。
package config

import (
	"fmt"
	"io/fs"
	"os"
)

// DataProgress 表示当前迁移阶段的完成量，Total 为零时页面显示不定进度。
type DataProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
}

// reportData 在提供回调时报告状态，兼容无需页面的测试和后台启动。
func reportData(report func(DataProgress), stage, message string, done, total int64) {
	if report != nil {
		report(DataProgress{stage, message, done, total})
	}
}

// dataCounter 为每个阶段单独累计实际处理字节。
func dataCounter(report func(DataProgress), stage, message string, total int64) func(int) {
	var done int64
	reportData(report, stage, message, done, total)
	return func(n int) { done += int64(n); reportData(report, stage, message, done, total) }
}

// dataSize 仅扫描文件元信息以计算进度总量，拒绝链接及特殊文件。
func dataSize(root string) (int64, error) {
	var total int64
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 || (!entry.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("数据中有链接或特殊文件，请先关闭浏览器并检查：%s", path)
		}
		if !entry.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// progressFS 包装标准文件系统，为 CopyFS 提供逐块复制进度。
type progressFS struct {
	fs.FS
	read func(int)
}

// Open 打开源文件并保持原有 Stat、Close 等文件系统语义。
func (p progressFS) Open(name string) (fs.File, error) {
	f, err := p.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return &progressFile{File: f, read: p.read}, nil
}

// progressFile 统计普通 Read 操作，避免快速复制路径绕过进度统计。
type progressFile struct {
	fs.File
	read func(int)
}

// Read 先执行实际读取，再报告成功读取的字节数。
func (p *progressFile) Read(b []byte) (int, error) {
	n, err := p.File.Read(b)
	if p.read != nil && n > 0 {
		p.read(n)
	}
	return n, err
}

// ReadDir 保留目录遍历能力，供标准 CopyFS 的 WalkDir 使用。
func (p *progressFile) ReadDir(n int) ([]fs.DirEntry, error) {
	f, ok := p.File.(fs.ReadDirFile)
	if !ok {
		return nil, fmt.Errorf("数据目录不支持遍历")
	}
	return f.ReadDir(n)
}
