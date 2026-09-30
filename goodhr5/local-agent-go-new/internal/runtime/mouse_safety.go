// Package runtime 文件作用：为已知 Camoufox 版本安装原生鼠标轨迹防卡死修正，不修改招聘页面。
package runtime

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
)

//go:embed mouse_safety.js
var mouseSafetyEvents []byte

//go:embed native_input_safety.js
var nativeInputSafety []byte

//go:embed wheel_safety.js
var wheelSafetyEvents []byte

const mouseHandlerResource = "chrome/juggler/content/protocol/PageHandler.js"
const nativeInputSafetyVersion = "v9"
const mouseSafetyMarker = "// GoodHR native mouse safety " + nativeInputSafetyVersion

// EnsureCamoufoxMouseSafety 修正已知协议资源，保留原始备份，未知版本不修改。
func EnsureCamoufoxMouseSafety(camoufoxDir string) error {
	archivePath, err := nativeInputArchivePath(camoufoxDir)
	if err != nil {
		return err
	}
	reader, err := zip.OpenReader(archivePath)
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
	for _, version := range []string{"v7", "v8"} {
		if bytes.HasPrefix(original, []byte("// GoodHR native mouse safety "+version+"\n")) {
			return upgradeMouseSafetyLegacy(original, version)
		}
	}
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v1\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v2\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v3\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v4\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v5\n"))
	original = bytes.TrimPrefix(original, []byte("// GoodHR native mouse safety v6\n"))
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
	// 原生鼠标的激活等待也纳入保护；未知方法结构不修改原始归档。
	mouseStart := []byte("  async ['Page.dispatchMouseEvent']({type, x, y, button, clickCount, modifiers, buttons}) {")
	wheelStart := []byte("  async ['Page.dispatchWheelEvent'](")
	wheelEnd := []byte("  async ['Page.insertText'](")
	if bytes.Count(result, mouseStart) != 1 || bytes.Count(result, wheelStart) != 1 || bytes.Count(result, wheelEnd) != 1 {
		return nil, fmt.Errorf("浏览器原生输入方法无法识别，原资源没有修改")
	}
	mouseFrom, wheelFrom, wheelTo := bytes.Index(result, mouseStart), bytes.Index(result, wheelStart), bytes.Index(result, wheelEnd)
	if wheelFrom <= mouseFrom || wheelTo <= wheelFrom {
		return nil, fmt.Errorf("浏览器原生输入方法顺序无法识别，原资源没有修改")
	}
	mouse := result[mouseFrom:wheelFrom]
	preamble := []byte("    const win = this._pageTarget._window;")
	activation := []byte("    await this._pageTarget.activateAndRun(async () => {")
	if bytes.Count(mouse, preamble) != 1 || bytes.Count(mouse, activation) != 1 {
		return nil, fmt.Errorf("浏览器鼠标激活方法无法识别，原资源没有修改")
	}
	mouse = bytes.Replace(mouse, preamble, append(append([]byte{}, preamble...), []byte("\n    const inputDiagnostic = this._goodhrInputDiagnostic(type, x, y, { buttons });\n    let guard = () => {};")...), 1)
	mouse = bytes.Replace(mouse, activation, []byte("    await this._goodhrRunInput(inputDiagnostic, async (inputGuard) => {\n      guard = inputGuard;\n      guard();"), 1)
	mouse = bytes.ReplaceAll(mouse, []byte("          await this._contentPage.send('dispatchDragEvent'"), []byte("          guard(); inputDiagnostic.stage = 'drag_event';\n          await this._contentPage.send('dispatchDragEvent'"))
	patched := append([]byte{}, result[:mouseFrom]...)
	patched = append(patched, nativeInputSafety...)
	patched = append(patched, '\n')
	patched = append(patched, mouse...)
	patched = append(patched, wheelSafetyEvents...)
	patched = append(patched, '\n')
	patched = append(patched, result[wheelTo:]...)
	result = patched
	// 回执超时时同时释放调用方的拖拽观察器，避免异常路径积累监听器。
	move := []byte("        await sendEvents(['mousemove']);")
	if bytes.Count(result, move) == 1 {
		result = bytes.Replace(result, move, []byte("        try { await sendEvents(['mousemove']); } catch (error) { watcher.dispose(); throw error; }"), 1)
	}
	return result, nil
}

// upgradeMouseSafetyLegacy 仅替换已知 v7/v8 的公共输入保护，保留鼠标、滚轮和其他浏览器资源修正。
func upgradeMouseSafetyLegacy(original []byte, version string) ([]byte, error) {
	if version != "v7" && version != "v8" {
		return nil, fmt.Errorf("浏览器输入保护版本不支持升级：%s", version)
	}
	helperStart := []byte("// 文件作用：给浏览器原生输入增加激活阶段保护和窗口证据，超时后禁止迟到的输入，不读取招聘页面。")
	mouseStart := []byte("  async ['Page.dispatchMouseEvent']({type, x, y, button, clickCount, modifiers, buttons}) {")
	from, to := bytes.Index(original, helperStart), bytes.Index(original, mouseStart)
	if from < 0 || to <= from || bytes.Count(original, helperStart) != 1 || bytes.Count(original, mouseStart) != 1 ||
		!bytes.Contains(original[from:to], []byte("_goodhrRunInput(diagnostic, operation)")) ||
		!bytes.Contains(original[to:], []byte("GOODHR_MOUSE_APZ_TIMEOUT:wheel")) ||
		!bytes.Contains(original[to:], []byte("GOODHR_MOUSE_ACK_TIMEOUT:")) {
		return nil, fmt.Errorf("浏览器 %s 输入保护结构无法识别，原资源没有修改", version)
	}
	updated := bytes.Replace(original[:from], []byte("// GoodHR native mouse safety "+version+"\n"), []byte(mouseSafetyMarker+"\n"), 1)
	updated = append(updated, nativeInputSafety...)
	updated = append(updated, '\n')
	updated = append(updated, original[to:]...)
	return updated, nil
}
