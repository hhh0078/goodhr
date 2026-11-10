// Package liepin 本文件从下载前后标签清单中识别且只识别本次原生回执对应的新文件页。
package liepin

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// newLiepinDownloadViewer 按本次已保存文件的完整路径匹配，已有同文件标签或新出现多个同文件标签均不允许关闭。
func newLiepinDownloadViewer(before, after contract.PageListResult, record contract.DownloadRecord) (*contract.DownloadViewerRestore, error) {
	if record.ID == "" || record.Status != "saved" || !filepath.IsAbs(record.FilePath) {
		return nil, nil
	}
	path := filepath.ToSlash(record.FilePath)
	matches := func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "file" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
			return false
		}
		name := u.Path
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			name = "//" + u.Host + name
		}
		// Windows 本地盘使用 file:///C:/...，UNC 共享保留上面的 //主机；Mac 直接按绝对路径比较。
		if len(path) > 2 && path[1] == ':' && strings.HasPrefix(name, "/") {
			name = name[1:]
		}
		return name == path
	}
	existed := false
	for _, p := range before.Pages {
		if matches(p.URL) {
			existed = true
			break
		}
	}
	viewer := ""
	for _, p := range after.Pages {
		if !matches(p.URL) {
			continue
		}
		if viewer != "" {
			return nil, fmt.Errorf("同一个下载文件对应多个标签，暂不操作")
		}
		viewer = p.URL
	}
	if viewer == "" || (existed && len(after.Pages) != 2) {
		return nil, nil
	}
	// 下载文件已移入简历库时，浏览器会复用相同路径与已有查看页。只有这两页时可回原页，但不能关闭旧文件页。
	return &contract.DownloadViewerRestore{ViewerURL: viewer, DownloadID: record.ID, KeepViewer: existed}, nil
}
