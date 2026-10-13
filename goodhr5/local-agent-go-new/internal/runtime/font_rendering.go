// Package runtime 文件作用：在 macOS 浏览器启动前修正独立 GPU 进程导致的中文字体渲染异常。
package runtime

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// camoufoxMacFontRenderingConfig 在 AutoConfig 阶段锁定设置，早于 Playwright 的 Browser.enable。
// Mozilla 2032466 记录了 macOS 独立 GPU 进程与字体渲染异常的关系。
const camoufoxMacFontRenderingConfig = `
// GoodHR macOS font rendering v1
lockPref("layers.gpu-process.enabled", false);
lockPref("layers.gpu-process.force-enabled", false);
`

// EnsureCamoufoxFontRendering 在 macOS 已有启动配置中禁用独立 GPU 进程，其他系统不读写资源。
func EnsureCamoufoxFontRendering(camoufoxDir string) error {
	return ensureCamoufoxFontRendering(camoufoxDir, goruntime.GOOS)
}

// ensureCamoufoxFontRendering 按目标系统准备早启动设置，并保留原配置及文件权限。
func ensureCamoufoxFontRendering(camoufoxDir string, platform string) error {
	if platform != "darwin" {
		return nil
	}
	if strings.TrimSpace(camoufoxDir) == "" {
		return fmt.Errorf("Camoufox 目录为空")
	}
	configPath := filepath.Join(camoufoxResourceDir(camoufoxDir), "camoufox.cfg")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取浏览器早启动配置 camoufox.cfg 失败：%w", err)
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return fmt.Errorf("浏览器早启动配置 camoufox.cfg 为空，原配置没有修改")
	}
	if bytes.HasSuffix(content, []byte(camoufoxMacFontRenderingConfig)) {
		return nil
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("读取浏览器早启动配置属性失败：%w", err)
	}
	// 先写同目录临时文件，避免写入中断时截断浏览器原有启动配置。
	temporary, err := os.CreateTemp(filepath.Dir(configPath), ".goodhr-font-rendering-*")
	if err != nil {
		return fmt.Errorf("准备浏览器字体修正失败：%w", err)
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("保留浏览器早启动配置权限失败：%w", err)
	}
	if _, err := temporary.Write(append(content, []byte(camoufoxMacFontRenderingConfig)...)); err != nil {
		return fmt.Errorf("写入浏览器字体修正失败：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("保存浏览器字体修正失败：%w", err)
	}
	if err := os.Rename(temporary.Name(), configPath); err != nil {
		return fmt.Errorf("替换浏览器早启动配置失败：%w", err)
	}
	return nil
}
