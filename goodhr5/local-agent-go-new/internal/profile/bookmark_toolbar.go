// Package profile 文件作用：把误放到地址栏的书签组件恢复到书签栏，保留其他工具栏布局。
package profile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// repairBookmarkToolbar 在下一次浏览器启动时修复空书签栏；修复已保存后移除临时布局覆盖。
func repairBookmarkToolbar(profileDir string, userPreferences []byte) ([]byte, error) {
	managed := regexp.MustCompile(`(?m)^// GoodHR：下一次启动恢复书签组件位置。\r?\nuser_pref\("browser\.uiCustomization\.state",[^\r\n]*\);\r?\n?`)
	userPreferences = managed.ReplaceAll(userPreferences, nil)
	prefs, err := os.ReadFile(filepath.Join(profileDir, "prefs.js"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	pattern := regexp.MustCompile(`(?m)^user_pref\("browser\.uiCustomization\.state", ("[^\r\n]*")\);\r?$`)
	// 用户明确设置的布局优先，其他情况下读取浏览器最后保存的布局。
	match := pattern.FindSubmatch(userPreferences)
	if match == nil {
		match = pattern.FindSubmatch(prefs)
	}
	if match == nil {
		return userPreferences, nil
	}
	var stateJSON string
	if json.Unmarshal(match[1], &stateJSON) != nil {
		return userPreferences, nil
	}
	var state map[string]json.RawMessage
	if json.Unmarshal([]byte(stateJSON), &state) != nil {
		return userPreferences, nil
	}
	var placements map[string][]string
	if json.Unmarshal(state["placements"], &placements) != nil || placements == nil {
		return userPreferences, nil
	}
	for _, widget := range placements["PersonalToolbar"] {
		if widget == "personal-bookmarks" {
			return userPreferences, nil
		}
	}
	for area, widgets := range placements {
		kept := make([]string, 0, len(widgets))
		for _, widget := range widgets {
			if widget != "personal-bookmarks" {
				kept = append(kept, widget)
			}
		}
		placements[area] = kept
	}
	placements["PersonalToolbar"] = append([]string{"personal-bookmarks"}, placements["PersonalToolbar"]...)
	state["placements"], err = json.Marshal(placements)
	if err != nil {
		return nil, err
	}
	encodedState, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	value, err := json.Marshal(string(encodedState))
	if err != nil {
		return nil, err
	}
	// 只临时覆盖修改后的布局，浏览器保存完成后下一次准备配置会清除此项。
	userPreferences = append(userPreferences, []byte("\n// GoodHR：下一次启动恢复书签组件位置。\nuser_pref(\"browser.uiCustomization.state\", ")...)
	userPreferences = append(userPreferences, value...)
	return append(userPreferences, []byte(");\n")...), nil
}
