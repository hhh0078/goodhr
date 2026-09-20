// Package runtime 文件作用：从主程序内置资源离线安装和修复 UBO，保留原目录供回退。
package runtime

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
)

// bundledUBO 保存固定版本的官方扩展，随 Go 主程序进入全部平台安装包。
//
//go:embed addons/ublock-origin-1.75.0.xpi
var bundledUBO []byte

// bundledUBOMu 避免多个启动请求同时检查和替换同一个扩展目录。
var bundledUBOMu sync.Mutex

// bundledUBOPath 返回与 Camoufox 官方资源布局一致的扩展路径。
func bundledUBOPath(camoufoxDir string, platform string) string {
	if platform == "darwin" {
		return filepath.Join(camoufoxDir, "Camoufox.app", "Contents", "Resources", "addons", "UBO")
	}
	return filepath.Join(camoufoxDir, "addons", "UBO")
}

// EnsureBundledUBO 校验每个扩展文件，缺失或损坏时离线恢复；不会访问网络或删除旧扩展。
func EnsureBundledUBO(camoufoxDir string) error {
	bundledUBOMu.Lock()
	defer bundledUBOMu.Unlock()
	if strings.TrimSpace(camoufoxDir) == "" {
		return fmt.Errorf("内置扩展的浏览器目录为空")
	}
	root, err := filepath.Abs(camoufoxDir)
	if err != nil {
		return err
	}
	target := bundledUBOPath(root, goruntime.GOOS)
	// 逐级检查父目录，拒绝通过链接把修复写入浏览器目录之外。
	parent := filepath.Dir(target)
	for current := parent; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("扩展目录不能是链接或文件：%s", current)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return fmt.Errorf("检查扩展目录 %s 失败：%w", current, statErr)
		}
		if current == root {
			break
		}
	}
	reader, err := zip.NewReader(bytes.NewReader(bundledUBO), int64(len(bundledUBO)))
	if err != nil {
		return fmt.Errorf("内置 UBO 压缩包无法读取：%w", err)
	}
	if err = verifyBundledUBO(target, reader); err == nil {
		return nil
	}
	log.Printf("[运行组件] UBO 文件需要恢复 path=%s reason=%v", target, err)
	if err = os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("创建扩展目录 %s 失败：%w", parent, err)
	}
	stage, err := os.MkdirTemp(parent, ".UBO-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archivePath := filepath.Join(stage, "addon.zip")
	if err = os.WriteFile(archivePath, bundledUBO, 0o600); err != nil {
		return err
	}
	payload := filepath.Join(stage, "payload")
	// 复用运行组件已有的安全 ZIP 解压能力。
	if err = extractZip(archivePath, payload, nil); err != nil {
		return fmt.Errorf("解压内置 UBO 失败：%w", err)
	}
	if err = verifyBundledUBO(payload, reader); err != nil {
		return fmt.Errorf("内置 UBO 解压校验失败：%w", err)
	}
	backup := ""
	if _, err = os.Lstat(target); err == nil {
		// 随机创建唯一备份名，避免覆盖用户手动保存的备份。
		backup, err = os.MkdirTemp(parent, "UBO-backup-")
		if err != nil {
			return err
		}
		if err = os.Remove(backup); err != nil {
			return err
		}
		if err = os.Rename(target, backup); err != nil {
			return fmt.Errorf("备份 UBO 目录 %s 失败：%w", target, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查 UBO 目录 %s 失败：%w", target, err)
	}
	if err = os.Rename(payload, target); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, target); restoreErr != nil {
				return fmt.Errorf("安装 UBO 失败：%v；原目录保留在 %s，恢复失败：%w", err, backup, restoreErr)
			}
		}
		return fmt.Errorf("安装 UBO 到 %s 失败：%w", target, err)
	}
	log.Printf("[运行组件] 内置 UBO 已离线恢复 version=1.75.0 path=%s backup=%s", target, backup)
	return nil
}

// verifyBundledUBO 按官方压缩包逐项检查大小和 CRC，识别缺失、同大小损坏和链接文件。
func verifyBundledUBO(directory string, reader *zip.Reader) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("UBO 目录不是普通目录：%s", directory)
	}
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		filePath, err := safeJoin(directory, entry.Name)
		if err != nil {
			return err
		}
		for parent := filepath.Dir(filePath); parent != directory; parent = filepath.Dir(parent) {
			info, err := os.Lstat(parent)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("UBO 子目录缺失或不是普通目录：%s", parent)
			}
		}
		info, err := os.Lstat(filePath)
		if err != nil {
			return fmt.Errorf("UBO 文件缺失：%s：%w", filePath, err)
		}
		if !info.Mode().IsRegular() || info.Size() != int64(entry.UncompressedSize64) {
			return fmt.Errorf("UBO 文件大小或类型不正确：%s", filePath)
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		digest := crc32.NewIEEE()
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if digest.Sum32() != entry.CRC32 {
			return fmt.Errorf("UBO 文件校验不通过：%s", filePath)
		}
	}
	return nil
}
