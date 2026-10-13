// Package runtime 文件作用：统一解析原地安装组件的相对路径，保留 Windows 文件名和 Mac .app、Vision OCR 适配。
package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// recordedExecutablePath 只使用运行目录内存在的安装记录，损坏或越界记录交给旧路径兜底。
func (m *Manager) recordedExecutablePath(component string) string {
	recorded := m.loadVersions()[component].ExecutablePath
	if recorded != "" {
		if path, err := safeJoin(m.runtimeDir, recorded); err == nil && fileExists(path) {
			return path
		}
	}
	return ""
}

// CamoufoxDir 返回当前浏览器的真实组件根目录，版本、语言包、扩展和 Worker 必须共用此路径。
func (m *Manager) CamoufoxDir() string {
	if configured := strings.TrimSpace(os.Getenv("CAMOUFOX_BINARY_PATH")); configured != "" {
		return camoufoxDirectoryForBinary(configured, goruntime.GOOS)
	}
	recorded := m.loadVersions()["camoufox"]
	if binary, err := safeJoin(m.runtimeDir, recorded.ExecutablePath); err == nil && fileExists(binary) {
		if root, err := safeJoin(m.runtimeDir, recorded.DirectoryPath); err == nil {
			if relative, err := filepath.Rel(root, binary); err == nil {
				if _, err := safeJoin(root, relative); err == nil {
					if info, err := os.Stat(root); err == nil && info.IsDir() {
						return root
					}
				}
			}
		}
		return camoufoxDirectoryForBinary(binary, goruntime.GOOS)
	}
	return filepath.Join(m.runtimeDir, "camoufox")
}

// camoufoxDirectoryForBinary 在 Mac 返回 .app 的父目录，Windows 则返回启动文件所在目录。
func camoufoxDirectoryForBinary(binaryPath string, system string) string {
	if system == "darwin" {
		for current := filepath.Dir(binaryPath); ; current = filepath.Dir(current) {
			if strings.HasSuffix(strings.ToLower(current), ".app") {
				return filepath.Dir(current)
			}
			if filepath.Dir(current) == current {
				break
			}
		}
	}
	return filepath.Dir(binaryPath)
}

// findCamoufoxBinary 按目标系统查找浏览器，Mac 必须保留完整的 .app/Contents/MacOS 结构。
func findCamoufoxBinary(root string, system string) string {
	switch system {
	case "darwin":
		return firstExistingFile(filepath.Join(root, "Camoufox.app", "Contents", "MacOS", "camoufox"), findFile(root, "camoufox"))
	case "windows":
		return firstExistingFile(findFile(root, "camoufox.exe"), findFile(root, "camoufox"))
	default:
		return firstExistingFile(findFile(root, "camoufox-bin"), findFile(root, "camoufox"))
	}
}

// findOCRBinary 查找当前系统的 OCR 文件名，兼容旧包中多层包装目录。
func findOCRBinary(root string, system string) string {
	names := []string{"RapidOCR-json", "RapidOCR_json", "rapidocr-json"}
	if system == "windows" {
		names = []string{"RapidOCR-json.exe", "RapidOCR_json.exe", "rapidocr-json.exe"}
	}
	for _, name := range names {
		if found := findFile(root, name); found != "" {
			return found
		}
	}
	return ""
}

// checkExecutableFile 检查非空普通文件；Unix 另检查执行权限，不把 Windows 权限规则套到 Mac。
func checkExecutableFile(path string, system string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("可执行文件不可用：%w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("可执行文件为空或不是普通文件：%s", path)
	}
	if system != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("可执行文件没有执行权限：%s", path)
	}
	return nil
}

// checkOCRFiles 检查 OCR 文件；Mac 使用系统 Vision，不要求 Windows 的 ONNX 模型。
func checkOCRFiles(binaryPath string, system string) error {
	if err := checkExecutableFile(binaryPath, system); err != nil {
		return err
	}
	if system != "darwin" && !fileExists(filepath.Join(filepath.Dir(binaryPath), "models", "ch_PP-OCRv3_det_infer.onnx")) {
		return fmt.Errorf("OCR 缺少识别模型")
	}
	return nil
}

// checkExtractedComponent 安装前检查真实新目录；Node 执行版本检查，浏览器和 OCR 检查文件与平台权限。
func checkExtractedComponent(ctx context.Context, component string, root string) (string, error) {
	system := goruntime.GOOS
	switch component {
	case "node_runtime":
		path := findFile(root, nodeBinaryName())
		return path, checkNodeExecutable(ctx, path)
	case "camoufox":
		path := findCamoufoxBinary(root, system)
		return path, checkExecutableFile(path, system)
	case "ocr":
		path := findOCRBinary(root, system)
		return path, checkOCRFiles(path, system)
	default:
		return "", fmt.Errorf("没有识别出运行组件：%s", component)
	}
}
