// Package boss 本文件验证联系人轻读与置顶头像分段补读，确保减少字段时不漏行、不串人、不污染已核实缓存。
package boss

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type changingBossContactsBrowser struct {
	*bossReplyTestBrowser
	before func(contract.ElementFindAllRequest)
	after  func(contract.ElementFindAllRequest, []contract.FindAllItem) []contract.FindAllItem
}

// FindAll 在真实窗口测试替身读取前后模拟列表重排或缺行，不涉及浏览器或真实账号。
func (b *changingBossContactsBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if b.before != nil {
		b.before(request)
	}
	rows, err := b.bossReplyTestBrowser.FindAll(ctx, request)
	if err == nil && b.after != nil {
		rows = b.after(request, rows)
	}
	return rows, err
}

// TestBossContactsReadOnlyIdentityThenPinnedAvatars 验证51行只轻读两字段，11置顶一次成段补头像，原生行不因置顶数量丢缓存。
func TestBossContactsReadOnlyIdentityThenPinnedAvatars(t *testing.T) {
	r, b, cfg := bossContactFixture(40)
	for i := 0; i < 10; i++ {
		b.contacts = append(b.contacts, contract.FindAllItem{Text: fmt.Sprintf("置顶%d 消息", i), Fields: map[string]string{"name": fmt.Sprintf("置顶%d", i), "avatar_url": fmt.Sprintf("https://img.test/extra/%d", i)}})
	}
	rows, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || len(rows) != 51 || len(b.requests) != 2 || len(r.contactRows) != 40 {
		t.Fatal("51行与11置顶读取不完整或缓存被清空", len(rows), len(b.requests), len(r.contactRows), err)
	}
	first, second := b.requests[0], b.requests[1]
	if len(first.Fields) != 2 || first.Fields["thread_id"].Description == "" || first.Fields["name"].Description == "" {
		t.Fatal("全表仍读取额外字段", first.Fields)
	}
	if second.StartIndex != 40 || second.MaxItems != 11 || len(second.Fields) != 3 || second.Fields["avatar_url"].Description == "" {
		t.Fatal("置顶头像没有成段补读", second)
	}
	if rows[0].AvatarURL != "" || rows[40].AvatarURL == "" || rows[0].Summary != b.contacts[0].Text {
		t.Fatal("头像扩大到原生全表，或摘要丢弃已核实正文")
	}
	// 对外结果不能反向改写上一份核实缓存。
	rows[0].Fields["name"] = "调用方改动"
	if r.contactRows[rows[0].PlatformThreadID].Fields["name"] != "候选人0" {
		t.Fatal("调用方修改污染了原生缓存")
	}
	b.requests = nil
	b.contacts[0].Text = "候选人0 新问题"
	next, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || len(b.requests) != 2 || next[0].Summary != "候选人0 新问题" || len(r.contactRows) != 40 {
		t.Fatal("新来信丢失或恢复了全表详情读取", err)
	}
}

// TestBossPinnedAvatarRefreshDoesNotReuseIdentity 验证头像变化时必须新读并生成新临时键，不能借用上轮的置顶身份。
func TestBossPinnedAvatarRefreshDoesNotReuseIdentity(t *testing.T) {
	r, b, cfg := bossContactFixture(2)
	before, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	b.contacts[2].Fields["avatar_url"] = "https://img.test/pinned-new"
	after, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || after[2].AvatarURL != "https://img.test/pinned-new" || before[2].Key == after[2].Key || len(r.contactRows) != 2 {
		t.Fatal("头像变化复用了旧置顶身份", err)
	}
}

// TestBossPinnedReadRetriesWholeSnapshotOnReorder 验证补头像期间置顶与原生行重排时重读整份轻快照，不按旧序号串人。
func TestBossPinnedReadRetriesWholeSnapshotOnReorder(t *testing.T) {
	r, b, cfg := bossContactFixture(8)
	changed := false
	wrapper := &changingBossContactsBrowser{bossReplyTestBrowser: b, before: func(request contract.ElementFindAllRequest) {
		if _, ok := request.Fields["avatar_url"]; ok && !changed {
			b.contacts[0], b.contacts[8] = b.contacts[8], b.contacts[0]
			changed = true
		}
	}}
	rows, err := r.readBossContacts(context.Background(), wrapper, cfg)
	if err != nil || len(rows) != 9 || rows[0].Name != "置顶候选人" || rows[8].PlatformThreadID != "1000-0" || rows[8].Index != 8 || len(b.requests) != 4 {
		t.Fatal("重排未重读或使用了旧索引", len(b.requests), err)
	}
}

// TestBossPinnedIncompleteSnapshotKeepsOldCache 验证补读总数、索引或正文变化均拒绝部分结果，并保留原核实缓存。
func TestBossPinnedIncompleteSnapshotKeepsOldCache(t *testing.T) {
	for _, kind := range []string{"total", "index", "text", "missing_avatar", "new_native_key", "missing_row"} {
		t.Run(kind, func(t *testing.T) {
			r, b, cfg := bossContactFixture(8)
			if _, err := r.readBossContacts(context.Background(), b, cfg); err != nil {
				t.Fatal(err)
			}
			saved := make(map[string]contract.FindAllItem, len(r.contactRows))
			for key, row := range r.contactRows {
				saved[key] = cloneBossContactRow(row)
			}
			wrapper := &changingBossContactsBrowser{bossReplyTestBrowser: b, after: func(request contract.ElementFindAllRequest, rows []contract.FindAllItem) []contract.FindAllItem {
				if _, ok := request.Fields["avatar_url"]; !ok || len(rows) == 0 {
					return rows
				}
				switch kind {
				case "total":
					rows[0].TotalMatches++
				case "index":
					rows[0].Index--
				case "text":
					rows[0].Text += "新消息"
				case "missing_avatar":
					rows[0].Fields["avatar_url"] = ""
				case "new_native_key":
					rows[0].Fields["thread_id"] = "9999-0"
				case "missing_row":
					return nil
				}
				return rows
			}}
			if rows, err := r.readBossContacts(context.Background(), wrapper, cfg); err == nil || rows != nil || !reflect.DeepEqual(saved, r.contactRows) {
				t.Fatal("不完整快照被接受或旧缓存遭污染", err)
			}
		})
	}
}

// TestBossContactsLeanScanFailsClosed 验证轻读仍拒绝姓名缺失、重复原生编号及超过5000行，不返回部分联系人。
func TestBossContactsLeanScanFailsClosed(t *testing.T) {
	for _, kind := range []string{"missing_name", "duplicate_id", "too_many"} {
		t.Run(kind, func(t *testing.T) {
			total := 2
			if kind == "too_many" {
				total = 5000
			}
			r, b, cfg := bossContactFixture(total)
			switch kind {
			case "missing_name":
				b.contacts[0].Fields["name"] = ""
			case "duplicate_id":
				b.contacts[1].Fields["thread_id"] = b.contacts[0].Fields["thread_id"]
			}
			rows, err := r.readBossContacts(context.Background(), b, cfg)
			if err == nil || rows != nil || len(r.contactRows) != 0 {
				t.Fatal("缺失、重复或超量联系人没有安全停止", err)
			}
		})
	}
}

var _ model.Browser = (*changingBossContactsBrowser)(nil)
