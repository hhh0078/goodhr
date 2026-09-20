// Package config 文件作用：迁移旧版 Windows 数据目录，校验完成后切换，并将运行临时文件留在数据盘。
package config

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

// PrepareData 在确认没有旧主程序运行后迁移数据，失败时禁止启动空白账号库。
func (c Config) PrepareData() error {
	return c.PrepareDataWithProgress(nil)
}

// PrepareDataWithProgress 在数据准备期间报告阶段与字节进度，不提前打开数据库或运行组件。
func (c Config) PrepareDataWithProgress(report func(DataProgress)) error {
	reportData(report, "checking", "正在检查本地数据是否需要搬家", 0, 0)
	if runtime.GOOS != "windows" {
		return c.EnsureDirectories()
	}
	if c.LegacyDataDir != "" && !strings.EqualFold(filepath.Clean(c.LegacyDataDir), filepath.Clean(c.DataDir)) {
		if err := migrateDataWithProgress(c.LegacyDataDir, c.DataDir, report); err != nil {
			return err
		}
	}
	if err := c.EnsureDirectories(); err != nil {
		return err
	}
	temp := filepath.Join(c.DataDir, "temp")
	cache := filepath.Join(c.DataDir, "cache")
	for _, directory := range []string{temp, cache} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
	}
	// 仅影响本进程与它启动的 OCR、Worker、浏览器，不修改系统环境变量。
	for key, value := range map[string]string{"TEMP": temp, "TMP": temp, "TMPDIR": temp, "XDG_CACHE_HOME": cache} {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// dataFileStamp 保存目录项和文件校验值，不记录文件内容。
type dataFileStamp struct {
	Directory bool
	Size      int64
	Hash      [32]byte
}

// dataInventory 拒绝链接和特殊文件，完整校验普通文件，避免迁移越过用户数据边界。
func dataInventory(root string) (map[string]dataFileStamp, error) {
	return dataInventoryProgress(root, nil)
}

// dataInventoryProgress 在逐块校验时报告读取量，不暴露文件名和文件内容。
func dataInventoryProgress(root string, read func(int)) (map[string]dataFileStamp, error) {
	result := make(map[string]dataFileStamp)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("数据中有链接或特殊文件，请先关闭浏览器并检查：%s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		stamp := dataFileStamp{Directory: entry.IsDir()}
		if !entry.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			hash := sha256.New()
			stamp.Size, err = io.Copy(hash, &progressFile{File: file, read: read})
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			copy(stamp.Hash[:], hash.Sum(nil))
		}
		result[relative] = stamp
		return nil
	})
	return result, err
}

// emptyDataTree 判断新目录是否只有空子目录，已有用户文件时绝不覆盖。
func emptyDataTree(root string) (bool, error) {
	empty := true
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			empty = false
		}
		return nil
	})
	return empty, err
}

// migrateData 先复制到目标盘临时目录并双向校验，再发布完整目录，最后清理已校验的旧副本。
func migrateData(source, destination string) error {
	return migrateDataWithProgress(source, destination, nil)
}

// migrateDataWithProgress 保留原有完整性保护，在扫描、复制和双向校验时报告实际进度。
func migrateDataWithProgress(source, destination string, report func(DataProgress)) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if strings.EqualFold(source, destination) || strings.HasPrefix(strings.ToLower(destination), strings.ToLower(source)+string(filepath.Separator)) || strings.HasPrefix(strings.ToLower(source), strings.ToLower(destination)+string(filepath.Separator)) {
		return fmt.Errorf("新旧数据目录不能相同或互相包含")
	}
	info, err := os.Lstat(source)
	if os.IsNotExist(err) {
		reportData(report, "skipped", "数据已在新位置，或这是首次使用，无需迁移", 0, 0)
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("旧数据目录不是普通目录：%s", source)
	}
	unlock, err := lockDataMigration(source)
	if err != nil {
		return err
	}
	defer unlock()
	empty, err := emptyDataTree(destination)
	if err != nil || !empty {
		if err == nil {
			reportData(report, "skipped", "正在使用安装目录中的数据，无需重复迁移", 0, 0)
		}
		return err // 新目录已有数据时继续使用，不合并两套账号库。
	}
	if err := ensureLegacyDataIdle(source); err != nil {
		return err
	}
	total, err := dataSize(source)
	if err != nil {
		return err
	}
	before, err := dataInventoryProgress(source, dataCounter(report, "verifying_source", "正在检查旧数据，先确认完整再搬家", total))
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".goodhr-data-migration-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // 仅清理本次在目标盘创建的临时目录。
	log.Printf("正在迁移 GoodHR 数据 source=%s destination=%s", source, destination)
	if err := os.CopyFS(stage, progressFS{FS: os.DirFS(source), read: dataCounter(report, "copying", "正在复制数据，原文件会先保留", total)}); err != nil {
		return fmt.Errorf("旧数据复制未完成，原文件已保留：%w", err)
	}
	copied, err := dataInventoryProgress(stage, dataCounter(report, "verifying_copy", "正在校验新副本，确认文件一个不少", total))
	if err != nil {
		return err
	}
	after, err := dataInventoryProgress(source, dataCounter(report, "verifying_original", "正在复核原数据，马上就好", total))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, copied) || !reflect.DeepEqual(before, after) {
		return fmt.Errorf("迁移校验未通过，原文件已保留，请关闭 GoodHR 和招聘浏览器后重试")
	}
	if err := ensureLegacyDataIdle(source); err != nil {
		return err
	}
	// 再检查一次，防止复制期间另一个启动实例写入新目录。
	if empty, err = emptyDataTree(destination); err != nil || !empty {
		return fmt.Errorf("新数据目录在迁移期间发生变化，原文件已保留")
	}
	if err := removeEmptyDataTree(destination); err != nil {
		return err
	}
	if err := os.Rename(stage, destination); err != nil {
		return fmt.Errorf("切换数据目录失败，原文件已保留：%w", err)
	}
	reportData(report, "cleanup", "新数据已就位，正在收尾", 0, 0)
	if err := os.RemoveAll(source); err != nil {
		// 完整新副本已发布，清理失败不回退到旧目录。
		log.Printf("新数据已启用，旧目录未清理完 source=%s error=%v", source, err)
	}
	log.Printf("GoodHR 数据迁移校验完成 destination=%s", destination)
	reportData(report, "migrated", "数据搬家完成", total, total)
	return nil
}

// removeEmptyDataTree 只用非递归删除移除空目录，目录中新出现文件时安全失败。
func removeEmptyDataTree(root string) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return fmt.Errorf("新数据目录已有文件，停止迁移：%s", entry.Name())
		}
		if err := removeEmptyDataTree(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return os.Remove(root)
}
