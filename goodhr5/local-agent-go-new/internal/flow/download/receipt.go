// Package download 本文件把同一原生下载监听的完成记录分给发起操作的流程或手动简历导入。
package download

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// downloadReceipts 记录当前浏览器的订阅及已被自动流程接收的文件编号。
type downloadReceipts struct {
	mu      sync.Mutex
	active  map[*downloadInterest]bool
	claimed map[string]bool
}

type downloadInterest struct {
	page, file   string
	fileKey      string
	ignoredQuery []string
	pendingID    string
	since        time.Time
	result       chan contract.DownloadReceipt
}

// downloadFileKey 生成文件比较键。默认保留完整链接；只在调用方明确指定时忽略临时查询参数。
func downloadFileKey(raw string, ignored []string) (string, bool) {
	if len(ignored) == 0 {
		return raw, true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") ||
		u.User != nil || strings.Contains(raw, "#") || u.Path == "" || u.Path == "/" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, `\`) {
		return "", false
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", false
	}
	// 同一参数出现两次时，不同服务可能分别选第一个或最后一个，无法可靠确认对象。
	for key, entries := range values {
		if key == "" || len(entries) != 1 {
			return "", false
		}
	}
	for _, key := range ignored {
		if key == "" {
			return "", false
		}
		values.Del(key)
	}
	u.Host = strings.ToLower(u.Host)
	u.RawQuery, u.ForceQuery = values.Encode(), false
	return u.String(), true
}

// WatchDownload 在真实点击前登记页面和下载链接。ignoredQuery 仅用于身份独立于临时签名的已核实文件。
func (m *Monitor) WatchDownload(page, file string, ignoredQuery ...string) (<-chan contract.DownloadReceipt, func(), error) {
	target, err := url.Parse(file)
	fileKey, valid := downloadFileKey(file, ignoredQuery)
	if err != nil || !valid || target.Host == "" || (target.Scheme != "https" && target.Scheme != "http") || page == "" {
		return nil, nil, fmt.Errorf("没有读到附件的下载地址，暂不认领文件")
	}
	hub := &m.receipts
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for sub := range hub.active {
		if sub.page != page {
			continue
		}
		// 两个订阅允许的签名参数可能不同；若存在某个地址能同时命中两者，就拒绝第二次登记。
		ignored := slices.Concat(sub.ignoredQuery, ignoredQuery)
		oldKey, oldOK := downloadFileKey(sub.file, ignored)
		newKey, newOK := downloadFileKey(file, ignored)
		if oldOK && newOK && oldKey == newKey {
			return nil, nil, fmt.Errorf("同一份附件正在等待下载")
		}
	}
	if hub.active == nil {
		hub.active = make(map[*downloadInterest]bool)
	}
	sub := &downloadInterest{
		page: page, file: file, fileKey: fileKey, ignoredQuery: slices.Clone(ignoredQuery),
		since: time.Now(), result: make(chan contract.DownloadReceipt, 2),
	}
	hub.active[sub] = true
	return sub.result, func() {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		// 已确认属于这个点击的在途下载即使稍后才落盘，也不应再次进入手动导入。
		if hub.active[sub] && sub.pendingID != "" {
			if hub.claimed == nil {
				hub.claimed = make(map[string]bool)
			}
			hub.claimed[sub.pendingID] = true
		}
		delete(hub.active, sub)
	}, nil
}

// deliver 在手动导入之前分发同页、同链接、登记后开始的下载；其他文件照常入队。
func (h *downloadReceipts) deliver(records []contract.DownloadRecord) []contract.DownloadRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.active) == 0 && len(h.claimed) == 0 {
		return records
	}
	current := make(map[string]bool, len(records))
	for _, record := range records {
		current[record.ID] = true
	}
	for id := range h.claimed {
		if !current[id] {
			delete(h.claimed, id)
		}
	}
	for sub := range h.active {
		var match *contract.DownloadRecord
		for i := range records {
			r := &records[i]
			if r.ID == "" || r.PageURL != sub.page || h.claimed[r.ID] {
				continue
			}
			if key, ok := downloadFileKey(r.URL, sub.ignoredQuery); !ok || key != sub.fileKey {
				continue
			}
			created, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
			if err != nil || created.Before(sub.since) {
				continue
			}
			if match != nil {
				sub.result <- contract.DownloadReceipt{Err: fmt.Errorf("同一链接同时产生了多份下载，无法确认附件归属")}
				delete(h.active, sub)
				match = nil
				break
			}
			match = r
		}
		if match != nil && sub.pendingID != "" && sub.pendingID != match.ID {
			sub.result <- contract.DownloadReceipt{Err: fmt.Errorf("已开始的下载编号发生变化，无法确认附件归属")}
			delete(h.active, sub)
			continue
		}
		if match != nil && match.Status == "pending" && sub.pendingID == "" {
			// 通道为首次在途及最终结果各留一格。后续轮询同一 pending 时不重复通知。
			sub.pendingID = match.ID
			sub.result <- contract.DownloadReceipt{Record: *match}
		}
		if match == nil || (match.Status != "saved" && match.Status != "failed") {
			continue
		}
		delete(h.active, sub)
		if match.Status == "failed" {
			sub.result <- contract.DownloadReceipt{Err: fmt.Errorf("浏览器报告附件下载失败")}
			continue
		}
		if h.claimed == nil {
			h.claimed = make(map[string]bool)
		}
		h.claimed[match.ID] = true
		sub.result <- contract.DownloadReceipt{Record: *match}
	}
	manual := make([]contract.DownloadRecord, 0, len(records))
	for _, record := range records {
		if !h.claimed[record.ID] {
			manual = append(manual, record)
		}
	}
	return manual
}
