// Package runtime 文件作用：为 Camoufox 启用可选的原生下载通道，并安装只观察状态的浏览器资源模块。
package runtime

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed native_downloads.js
var nativeDownloadsModule []byte

const nativeRegistryResource = "chrome/juggler/content/TargetRegistry.js"
const nativeHandlerResource = "chrome/juggler/content/protocol/BrowserHandler.js"
const nativeObserverResource = "chrome/juggler/content/GoodHRDownloads.js"
const nativeDownloadMarker = "// GoodHR native downloads v1"

// EnsureCamoufoxNativeDownloads 在受支持的浏览器资源中安装原生下载观察器，未知版本不强行修改。
func EnsureCamoufoxNativeDownloads(camoufoxDir string) error {
	if strings.TrimSpace(camoufoxDir) == "" {
		return fmt.Errorf("Camoufox 目录为空")
	}
	archivePath := firstExistingFile(
		filepath.Join(camoufoxDir, "omni.ja"),
		filepath.Join(camoufoxDir, "Camoufox.app", "Contents", "Resources", "omni.ja"),
		filepath.Join(camoufoxDir, "Contents", "Resources", "omni.ja"),
	)
	reader, err := zip.OpenReader(archivePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取浏览器下载资源失败：%w", err)
	}
	defer reader.Close()
	var registry, handler, observer []byte
	for _, file := range reader.File {
		if file.Name != nativeRegistryResource && file.Name != nativeObserverResource && file.Name != nativeHandlerResource {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return err
		}
		content, readErr := io.ReadAll(stream)
		stream.Close()
		if readErr != nil {
			return readErr
		}
		if file.Name == nativeRegistryResource {
			registry = content
		} else if file.Name == nativeHandlerResource {
			handler = content
		} else {
			observer = content
		}
	}
	if len(registry) == 0 {
		return fmt.Errorf("浏览器缺少标准下载通道资源")
	}
	updated := registry
	if !bytes.Contains(registry, []byte(nativeDownloadMarker)) {
		intercept := "interceptDownloadRequest(externalAppHandler, request, browsingContext, outFile) {"
		if bytes.Count(registry, []byte(intercept)) != 1 {
			return fmt.Errorf("浏览器下载资源版本无法识别，原资源没有修改")
		}
		updated = append([]byte(nativeDownloadMarker+"\n"), registry...)
		updated = bytes.Replace(updated, []byte(intercept), []byte(intercept+"\n    if (Services.prefs.getBoolPref('goodhr.downloads.native', false)) return false;"), 1)
	}
	updatedHandler := handler
	if !bytes.Contains(handler, []byte(nativeDownloadMarker)) {
		anchor := "    this._eventListeners = [\n"
		if bytes.Count(handler, []byte(anchor)) != 1 {
			return fmt.Errorf("浏览器下载初始化版本无法识别，原资源没有修改")
		}
		updatedHandler = append([]byte(nativeDownloadMarker+"\nconst {startGoodHRNativeDownloads} = ChromeUtils.importESModule('chrome://juggler/content/GoodHRDownloads.js');\n"), handler...)
		updatedHandler = bytes.Replace(updatedHandler, []byte(anchor), []byte("    if (Services.prefs.getBoolPref('goodhr.downloads.native', false)) await startGoodHRNativeDownloads();\n\n"+anchor), 1)
	}
	if bytes.Equal(updated, registry) && bytes.Equal(updatedHandler, handler) && bytes.Equal(observer, nativeDownloadsModule) {
		return nil
	}
	if err := reader.Close(); err != nil {
		return err
	}
	return replaceBrowserArchiveResources(archivePath, map[string][]byte{nativeRegistryResource: updated, nativeHandlerResource: updatedHandler, nativeObserverResource: nativeDownloadsModule})
}
