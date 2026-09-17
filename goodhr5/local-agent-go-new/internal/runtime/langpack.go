// Package runtime 文件作用：把随程序打包的 Firefox 简体中文语言包安装进 Camoufox，让浏览器界面显示中文。
package runtime

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// langpackBinary 是随程序一起打包的 Firefox 简体中文语言包。
// 语言包和 Firefox 大版本强绑定，只能用于 langpackFirefoxMajor 对应的 Camoufox。
//
//go:embed langpack/firefox-langpack-zh-CN.xpi
var langpackBinary []byte

// langpackExtensionID 是 Firefox 简体中文语言包的固定扩展编号。
const langpackExtensionID = "langpack-zh-CN@firefox.mozilla.org"

// langpackFirefoxMajor 是内置语言包支持的 Firefox 大版本，
// 与语言包 manifest 里的 strict_min_version / strict_max_version 保持一致。
const langpackFirefoxMajor = "152"

// EnsureChineseLanguagePack 把内置中文语言包写入 Camoufox 的应用级扩展目录。
// Camoufox 只打包英文界面资源，装上语言包并请求 zh-CN 后界面才会变成中文；
// 版本不匹配或写入失败时返回错误，调用方只记日志，不影响浏览器启动。
func EnsureChineseLanguagePack(camoufoxDir string) error {
	camoufoxDir = strings.TrimSpace(camoufoxDir)
	if camoufoxDir == "" {
		return fmt.Errorf("Camoufox 目录为空")
	}
	major, err := camoufoxMajorVersion(camoufoxDir)
	if err != nil {
		return err
	}
	if major != langpackFirefoxMajor {
		return fmt.Errorf("Camoufox 大版本 %s 与内置中文语言包 %s 不匹配，本次跳过", major, langpackFirefoxMajor)
	}
	targetDir := filepath.Join(camoufoxDir, "browser", "extensions")
	targetPath := filepath.Join(targetDir, langpackExtensionID+".xpi")
	if info, statErr := os.Stat(targetPath); statErr == nil && info.Size() == int64(len(langpackBinary)) {
		return nil
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建语言包目录失败：%w", err)
	}
	if err := os.WriteFile(targetPath, langpackBinary, 0o644); err != nil {
		return fmt.Errorf("写入中文语言包失败：%w", err)
	}
	return nil
}

// camoufoxMajorVersion 读取 Camoufox 根目录的 version.json 并返回 Firefox 大版本。
func camoufoxMajorVersion(camoufoxDir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(camoufoxDir, "version.json"))
	if err != nil {
		return "", fmt.Errorf("读取 Camoufox 版本失败：%w", err)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return "", fmt.Errorf("解析 Camoufox 版本失败：%w", err)
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return "", fmt.Errorf("Camoufox 版本为空")
	}
	return strings.SplitN(version, ".", 2)[0], nil
}
