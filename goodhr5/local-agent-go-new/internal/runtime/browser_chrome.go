// Package runtime 文件作用：修正 Camoufox 自带主题的标签点击区域，不改变网页内容或字体指纹。
package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// camoufoxTabInteractionCSS 覆盖上游将标签页用作窗口拖动区的样式。
const camoufoxTabInteractionCSS = `
/* GoodHR：恢复浏览器标签交互，仅作用于浏览器工具栏。 */
#TabsToolbar .tabbrowser-tab,
#TabsToolbar .tab-content,
#TabsToolbar .tab-close-button,
#TabsToolbar .toolbarbutton-1,
#TabsToolbar .titlebar-buttonbox-container {
  -moz-window-dragging: no-drag !important;
}
#TabsToolbar .tab-content {
  pointer-events: auto !important;
}
/* 尊重用户显示或隐藏书签栏的选择，覆盖上游主题的强制隐藏。 */
#navigator-toolbox #PersonalToolbar:not([collapsed="true"]) {
  display: flex !important;
}
`

// EnsureCamoufoxTabInteraction 补齐标签点击样式；重复调用不会重复写入，下载新组件后会重新补齐。
func EnsureCamoufoxTabInteraction(camoufoxDir string) error {
	if strings.TrimSpace(camoufoxDir) == "" {
		return fmt.Errorf("Camoufox 目录为空")
	}
	stylePath := filepath.Join(camoufoxDir, "chrome.css")
	content, err := os.ReadFile(stylePath)
	if os.IsNotExist(err) {
		return nil // 不带自定义主题的版本无需修正。
	}
	if err != nil {
		return fmt.Errorf("读取浏览器主题失败：%w", err)
	}
	if bytes.HasSuffix(content, []byte(camoufoxTabInteractionCSS)) {
		return nil
	}
	info, err := os.Stat(stylePath)
	if err != nil {
		return fmt.Errorf("读取浏览器主题属性失败：%w", err)
	}
	if err := os.WriteFile(stylePath, append(content, []byte(camoufoxTabInteractionCSS)...), info.Mode().Perm()); err != nil {
		return fmt.Errorf("修正浏览器标签交互失败：%w", err)
	}
	return nil
}

// EnsureCamoufoxBookmarkToolbar 修正组件自带的隐藏书签栏策略，保留所有其他浏览器策略。
func EnsureCamoufoxBookmarkToolbar(camoufoxDir string) error {
	if strings.TrimSpace(camoufoxDir) == "" {
		return fmt.Errorf("Camoufox 目录为空")
	}
	policyPath := filepath.Join(camoufoxDir, "distribution", "policies.json")
	content, err := os.ReadFile(policyPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取浏览器策略失败：%w", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		return err
	}
	var policies map[string]json.RawMessage
	if err := json.Unmarshal(document["policies"], &policies); err != nil {
		return err
	}
	if policies == nil {
		return fmt.Errorf("浏览器策略内容为空")
	}
	var visibility string
	if json.Unmarshal(policies["DisplayBookmarksToolbar"], &visibility) == nil && visibility == "always" {
		return nil
	}
	policies["DisplayBookmarksToolbar"] = json.RawMessage(`"always"`)
	document["policies"], err = json.Marshal(policies)
	if err != nil {
		return err
	}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	info, err := os.Stat(policyPath)
	if err != nil {
		return err
	}
	return os.WriteFile(policyPath, updated, info.Mode().Perm())
}
