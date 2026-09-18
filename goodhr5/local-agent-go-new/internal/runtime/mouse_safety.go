// Package runtime 文件作用：为已知 Camoufox 版本安装原生鼠标轨迹防卡死修正，不修改招聘页面。
package runtime

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed mouse_safety.js
var mouseSafetyEvents []byte

const mouseHandlerResource = "chrome/juggler/content/protocol/PageHandler.js"
const mouseSafetyMarker = "// GoodHR native mouse safety v5"

// EnsureCamoufoxMouseSafety 修正已知协议资源，保留原始备份，未知版本不修改。
func EnsureCamoufoxMouseSafety(camoufoxDir string) error {
	archivePath := firstExistingFile(filepath.Join(camoufoxDir, "omni.ja"), filepath.Join(camoufoxDir, "Camoufox.app", "Contents", "Resources", "omni.ja"), filepath.Join(camoufoxDir, "Contents", "Resources", "omni.ja"))
	reader, err := zip.OpenReader(archivePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, entry := range reader.File {
		if entry.Name != mouseHandlerResource {
			continue
		}
		stream, err := entry.Open()
		if err != nil {
			return err
		}
		original, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			return err
		}
		updated, err := patchMouseSafety(original)
		if err != nil {
			return err
		}
		if bytes.Equal(original, updated) {
			return nil
		}
		if err := reader.Close(); err != nil {
			return err
		}
		return replaceBrowserArchiveResources(archivePath, map[string][]byte{mouseHandlerResource: updated})
	}
	return fmt.Errorf("浏览器缺少标准鼠标协议资源")
}

// patchMouseSafety 只替换已验证的事件发送方法，并确保重复安装不改变资源。
func patchMouseSafety(original []byte) ([]byte, error) {
	if bytes.Contains(original, []byte(mouseSafetyMarker)) {
		return original, nil
	}
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v1\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v2\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v3\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v4\n"))
	start := []byte("    const sendEvents = async (types) => {")
	end := []byte("\n    // We must switch to proper tab in the tabbed browser so that")
	if bytes.Count(original, start) != 1 || bytes.Count(original, end) != 1 {
		return nil, fmt.Errorf("浏览器鼠标资源版本无法识别，原资源没有修改")
	}
	from, to := bytes.Index(original, start), bytes.Index(original, end)
	if to <= from || !bytes.Contains(original[from:to], []byte("ChromeUtils.camouGetMouseTrajectory")) || !bytes.Contains(original[from:to], []byte("watcher.ensureEvent(eventType")) {
		return nil, fmt.Errorf("浏览器拟人鼠标实现无法识别，原资源没有修改")
	}
	result := append([]byte(mouseSafetyMarker+"\n"), original[:from]...)
	result = append(result, mouseSafetyEvents...)
	result = append(result, original[to:]...)
	// 回执超时时同时释放调用方的拖拽观察器，避免异常路径积累监听器。
	move := []byte("        await sendEvents(['mousemove']);")
	if bytes.Count(result, move) == 1 {
		result = bytes.Replace(result, move, []byte("        try { await sendEvents(['mousemove']); } catch (error) { watcher.dispose(); throw error; }"), 1)
	}
	return result, nil
}
