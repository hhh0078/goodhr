// 文件作用：安全解压浏览器插件，并校验 Manifest 与目录内容摘要。
package browserextension

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type extensionManifest struct {
	ManifestVersion int    `json:"manifest_version"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Background      struct {
		ServiceWorker string   `json:"service_worker"`
		Scripts       []string `json:"scripts"`
	} `json:"background"`
	BrowserSpecificSettings struct {
		Gecko struct {
			ID string `json:"id"`
		} `json:"gecko"`
	} `json:"browser_specific_settings"`
}

// extractArchive 安全解压普通文件和目录，并限制条目数量及解压后总大小。
func extractArchive(archivePath string, targetDir string, maxExtractedBytes int64) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开插件压缩包失败：%w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > defaultMaxArchiveEntries {
		return fmt.Errorf("插件压缩包条目数量不正确")
	}
	var extracted int64
	for _, entry := range reader.File {
		targetPath, pathErr := safeArchivePath(targetDir, entry.Name)
		if pathErr != nil {
			return pathErr
		}
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !entry.FileInfo().IsDir()) {
			return fmt.Errorf("插件压缩包包含不支持的文件类型：%s", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("创建插件子目录失败：%w", err)
			}
			continue
		}
		if entry.UncompressedSize64 > uint64(maxExtractedBytes) || extracted > maxExtractedBytes-int64(entry.UncompressedSize64) {
			return fmt.Errorf("浏览器插件解压后超过大小限制")
		}
		if err = os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("创建插件子目录失败：%w", err)
		}
		if err = extractFile(entry, targetPath, maxExtractedBytes-extracted); err != nil {
			return err
		}
		extracted += int64(entry.UncompressedSize64)
	}
	return nil
}

// safeArchivePath 阻止 Zip Slip、绝对路径和 Windows 驱动器路径写出目标目录。
func safeArchivePath(targetDir string, name string) (string, error) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	cleanName := path.Clean(normalized)
	if strings.TrimSpace(normalized) == "" || strings.ContainsRune(normalized, 0) || strings.HasPrefix(normalized, "/") || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", fmt.Errorf("插件压缩包包含不安全路径：%s", name)
	}
	for _, segment := range strings.Split(cleanName, "/") {
		if unsafeWindowsPathSegment(segment) {
			return "", fmt.Errorf("插件压缩包包含 Windows 不安全文件名：%s", name)
		}
	}
	root, err := filepath.Abs(targetDir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(cleanName)))
	if err != nil {
		return "", err
	}
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("插件压缩包包含越界路径：%s", name)
	}
	return target, nil
}

// unsafeWindowsPathSegment 拒绝 NTFS 数据流、尾随点空格和 Windows 保留设备名。
func unsafeWindowsPathSegment(segment string) bool {
	if segment == "" || strings.Contains(segment, ":") || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return true
	}
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(segment, ".", 2)[0], " ."))
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CLOCK$" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

// extractFile 把单个压缩条目限量写入目标文件。
func extractFile(entry *zip.File, targetPath string, remaining int64) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("读取插件文件失败：%w", err)
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("创建插件文件失败：%w", err)
	}
	written, copyErr := io.Copy(target, io.LimitReader(source, remaining+1))
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil || written > remaining {
		_ = os.Remove(targetPath)
		if written > remaining {
			return fmt.Errorf("浏览器插件解压后超过大小限制")
		}
		if copyErr != nil {
			return fmt.Errorf("写入插件文件失败：%w", copyErr)
		}
		return fmt.Errorf("关闭插件文件失败：%w", closeErr)
	}
	return nil
}

// readManifest 读取并校验根 manifest.json 的必要字段。
func readManifest(manifestPath string) (extensionManifest, error) {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return extensionManifest{}, err
	}
	if len(content) > 1<<20 {
		return extensionManifest{}, fmt.Errorf("manifest.json 超过大小限制")
	}
	var manifest extensionManifest
	if err = json.Unmarshal(content, &manifest); err != nil {
		return extensionManifest{}, err
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.Background.ServiceWorker = strings.TrimSpace(manifest.Background.ServiceWorker)
	for index := range manifest.Background.Scripts {
		manifest.Background.Scripts[index] = strings.TrimSpace(manifest.Background.Scripts[index])
	}
	manifest.BrowserSpecificSettings.Gecko.ID = strings.TrimSpace(manifest.BrowserSpecificSettings.Gecko.ID)
	if manifest.Name == "" || manifest.Version == "" {
		return extensionManifest{}, fmt.Errorf("manifest.json 缺少名称或版本")
	}
	return manifest, nil
}

// directoryTreeSHA256 按规范化相对路径排序计算目录内容摘要，排除安装器自己的收据和待加载标记。
func directoryTreeSHA256(root string) (string, error) {
	files := make([]string, 0, 32)
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		relative, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		if relative == receiptFilename || relative == loadRequiredFilename {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("插件目录包含符号链接：%s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("插件目录包含不支持的文件类型：%s", relative)
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	hash := sha256.New()
	for _, relative := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(relative))
		info, statErr := os.Stat(fullPath)
		if statErr != nil {
			return "", statErr
		}
		if _, err = fmt.Fprintf(hash, "%d:%s:%d:", len(relative), relative, info.Size()); err != nil {
			return "", err
		}
		file, openErr := os.Open(fullPath)
		if openErr != nil {
			return "", openErr
		}
		written, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if written != info.Size() {
			return "", fmt.Errorf("计算插件目录摘要时文件发生变化：%s", relative)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// validateChromeManifest 核对固定产品身份、Manifest V3 后台入口和入口文件。
func validateChromeManifest(root string, manifest extensionManifest) error {
	if manifest.Name != expectedExtensionName {
		return fmt.Errorf("插件身份校验失败：manifest 名称不正确")
	}
	if manifest.ManifestVersion != 3 || manifest.Background.ServiceWorker == "" {
		return fmt.Errorf("插件格式校验失败：不是可用的 Chrome Manifest V3 包")
	}
	workerPath, err := safeArchivePath(root, manifest.Background.ServiceWorker)
	if err != nil {
		return fmt.Errorf("插件后台入口不安全：%w", err)
	}
	info, err := os.Stat(workerPath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("插件后台入口文件不存在")
	}
	return nil
}

// validateFirefoxManifest 核对固定产品身份、Mozilla 扩展编号、后台脚本和签名信封文件。
func validateFirefoxManifest(root string, manifest extensionManifest) error {
	if manifest.Name != expectedExtensionName {
		return fmt.Errorf("插件身份校验失败：manifest 名称不正确")
	}
	if manifest.ManifestVersion != 3 || len(manifest.Background.Scripts) == 0 {
		return fmt.Errorf("插件格式校验失败：不是可用的 Firefox Manifest V3 包")
	}
	if manifest.BrowserSpecificSettings.Gecko.ID != expectedFirefoxID {
		return fmt.Errorf("插件身份校验失败：Firefox 扩展编号不正确")
	}
	for _, script := range manifest.Background.Scripts {
		if script == "" {
			return fmt.Errorf("插件格式校验失败：Firefox 后台入口不能为空")
		}
		if err := validateRegularArchiveFile(root, script, "Firefox 后台入口"); err != nil {
			return err
		}
	}
	for _, signatureFile := range []string{
		"META-INF/manifest.mf",
		"META-INF/mozilla.sf",
		"META-INF/mozilla.rsa",
		"META-INF/cose.manifest",
		"META-INF/cose.sig",
	} {
		if err := validateRegularArchiveFile(root, signatureFile, "Mozilla 签名信封"); err != nil {
			return err
		}
	}
	return nil
}

// validateRegularArchiveFile 校验插件清单引用的相对路径安全，且目标是非空普通文件。
func validateRegularArchiveFile(root string, relative string, label string) error {
	targetPath, err := safeArchivePath(root, relative)
	if err != nil {
		return fmt.Errorf("%s不安全：%w", label, err)
	}
	info, err := os.Stat(targetPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("%s文件不存在或为空：%s", label, relative)
	}
	return nil
}
