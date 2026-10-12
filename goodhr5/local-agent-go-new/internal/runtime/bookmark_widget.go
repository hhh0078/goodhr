// Package runtime 文件作用：把浏览器书签组件固定在书签栏，避免 Camoufox 启动时将其移入地址栏。
package runtime

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const browserLayoutResource = "chrome/browser/content/browser/browser.xhtml"

// EnsureCamoufoxBookmarkWidget 修正浏览器自身布局资源，原样复制其他 ZIP 条目并保留原始资源备份。
func EnsureCamoufoxBookmarkWidget(camoufoxDir string) error {
	if strings.TrimSpace(camoufoxDir) == "" {
		return fmt.Errorf("Camoufox 目录为空")
	}
	archivePath := filepath.Join(camoufoxResourceDir(camoufoxDir), "browser", "omni.ja")
	info, err := os.Stat(archivePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	reader, err := zip.OpenReader(archivePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取浏览器布局包失败：%w", err)
	}
	defer reader.Close()
	var target *zip.File
	var original []byte
	for _, file := range reader.File {
		if file.Name != browserLayoutResource {
			continue
		}
		target = file
		stream, err := file.Open()
		if err != nil {
			return err
		}
		original, err = io.ReadAll(stream)
		stream.Close()
		if err != nil {
			return err
		}
		break
	}
	if target == nil {
		return fmt.Errorf("浏览器布局包缺少书签组件资源")
	}
	pattern := regexp.MustCompile(`<toolbaritem\s+id="personal-bookmarks"[^>]*>`)
	tag := pattern.Find(original)
	if tag == nil {
		return fmt.Errorf("浏览器布局不包含标准书签组件")
	}
	if bytes.Contains(tag, []byte(`removable="false"`)) {
		return nil
	}
	if !bytes.Contains(tag, []byte(`removable="true"`)) {
		return fmt.Errorf("书签组件布局属性无法识别")
	}
	updatedTag := bytes.Replace(tag, []byte(`removable="true"`), []byte(`removable="false"`), 1)
	updated := bytes.Replace(original, tag, updatedTag, 1)
	temporary, err := os.CreateTemp(filepath.Dir(archivePath), "goodhr-layout-*.ja")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	writer := zip.NewWriter(temporary)
	for _, file := range reader.File {
		if file == target {
			header := file.FileHeader
			stream, err := writer.CreateHeader(&header)
			if err == nil {
				_, err = stream.Write(updated)
			}
			if err != nil {
				writer.Close()
				temporary.Close()
				return err
			}
		} else {
			header := file.FileHeader
			stream, err := writer.CreateRaw(&header)
			if err != nil {
				writer.Close()
				temporary.Close()
				return err
			}
			raw, err := file.OpenRaw()
			if err == nil {
				_, err = io.Copy(stream, raw)
			}
			if err != nil {
				writer.Close()
				temporary.Close()
				return err
			}
		}
	}
	if err := writer.Close(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	if err := reader.Close(); err != nil {
		return err
	}
	backup := archivePath + ".goodhr-original"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		content, err := os.ReadFile(archivePath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(backup, content, 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), archivePath); err != nil {
		return fmt.Errorf("替换浏览器布局失败，请先关闭浏览器：%w", err)
	}
	return nil
}
