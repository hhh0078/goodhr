// Package storage 本文件只保存列表行匿名指纹及待核对状态，聊天的实际入库和回复去重仍由消息表负责。
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

// AutoReplyListObservation 是一行平台可见线索，仅对哈希进行持久化。
type AutoReplyListObservation struct {
	ThreadID, Signature string
	PreviewDigest       string
	ActivityTime        *time.Time
}

// AutoReplyListReview 是本次核对凭据；同分钟时仅 WitnessDigest 可证明预览刚刚改变。
type AutoReplyListReview struct {
	ThreadID, Signature, WitnessDigest string
	After                              time.Time
	Revision                           int64
	ReserveTurn                        bool   // 此轮合并本地待办时，给这一条久未核对的会话一个名额。
	binding                            string // 防止将一个账号或限定模式的回执传给另一种扫描范围。
}

type autoReplyListRow struct {
	signature, preview, witness string
	revision, after, checked    int64
	pending                     bool
}

// listDigest 计算稳定摘要，数据库中不保存联系人或其最后消息的明文。
func listDigest(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// listPreviewDigest 只接受调用方从真实可见预览算出的 SHA256，禁止把正文当摘要入库。
func listPreviewDigest(s string) string {
	if len(s) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(s); err != nil {
		return ""
	}
	return strings.ToLower(s)
}

// savedListRows 一次取出该账号和范围的轻量基线，避免为每位联系人重复查询。
func (t *AutoReplyTx) savedListRows(platform, account, scope string) (map[string]autoReplyListRow, error) {
	rs, err := t.tx.QueryContext(t.ctx, `SELECT thread_hash,signature,preview_digest,witness_digest,revision,pending,after_ns,checked_ns FROM auto_reply_list_rows WHERE owner_id=? AND platform_id=? AND account_id=? AND scope_hash=?`, t.owner, platform, account, scope)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	rows := map[string]autoReplyListRow{}
	for rs.Next() {
		var id string
		var r autoReplyListRow
		if err := rs.Scan(&id, &r.signature, &r.preview, &r.witness, &r.revision, &r.pending, &r.after, &r.checked); err != nil {
			return nil, err
		}
		rows[id] = r
	}
	return rows, rs.Err()
}

// knownListThreads 返回已真实采集过末条消息的线程；采集意图和空简历都不能当作已有聊天游标。
func (t *AutoReplyTx) knownListThreads(platform, account, target string) (map[string]bool, error) {
	rs, err := t.tx.QueryContext(t.ctx, `SELECT thread_id FROM auto_reply_conversations WHERE owner_id=? AND platform_id=? AND account_id=? AND (?='' OR thread_id=?) AND COALESCE(json_extract(payload_json,'$.last_synced_message_key'),'')!=''`, t.owner, platform, account, target, target)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	result := map[string]bool{}
	for rs.Next() {
		var id string
		if err := rs.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rs.Err()
}

// saveListRow 同事务保存新快照和未处理标记；普通轮询中没有变化的行不写库。
func (t *AutoReplyTx) saveListRow(platform, account, scope, id string, r autoReplyListRow) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO auto_reply_list_rows(owner_id,platform_id,account_id,scope_hash,thread_hash,signature,preview_digest,witness_digest,revision,pending,after_ns,checked_ns) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(owner_id,platform_id,account_id,scope_hash,thread_hash) DO UPDATE SET signature=excluded.signature,preview_digest=excluded.preview_digest,witness_digest=excluded.witness_digest,revision=excluded.revision,pending=excluded.pending,after_ns=excluded.after_ns,checked_ns=excluded.checked_ns`, t.owner, platform, account, scope, id, r.signature, r.preview, r.witness, r.revision, r.pending, r.after, r.checked)
	return err
}

// listReview 把本行待办转为可信核对凭据，零边界可交给已知消息游标处理。
func listReview(o AutoReplyListObservation, r autoReplyListRow, binding string) AutoReplyListReview {
	v := AutoReplyListReview{ThreadID: o.ThreadID, Signature: o.Signature, WitnessDigest: r.witness, Revision: r.revision, binding: binding}
	if r.after > 0 {
		v.After = time.Unix(0, r.after).UTC()
	}
	return v
}

// ObserveAutoReplyList 原子记录整份稳定快照，重启后未核对的线程仍会返回；限定模式忽略所有范围外的行。
func (t *AutoReplyTx) ObserveAutoReplyList(platform, account, target string, items []AutoReplyListObservation, now time.Time) ([]AutoReplyListReview, error) {
	if platform == "" || account == "" || now.IsZero() || now.UnixNano() <= 0 {
		return nil, fmt.Errorf("列表扫描需要明确平台、账号和观测时刻")
	}
	scope := listDigest(target)
	binding := listDigest(t.owner + "\x00" + platform + "\x00" + account + "\x00" + scope)
	// 分别记本范围的扫描时刻和上一次让位时刻，长时间运行的一轮也不会令连续两轮都抢占名额。
	rotationScope := "rotation/" + scope
	var previous, lastReserved int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT COALESCE(MAX(CASE WHEN scope_hash=? THEN scanned_ns END),0),
		COALESCE(MAX(CASE WHEN scope_hash=? THEN scanned_ns END),0) FROM auto_reply_list_scopes
		WHERE owner_id=? AND platform_id=? AND account_id=? AND scope_hash IN (?,?)`,
		scope, rotationScope, t.owner, platform, account, scope, rotationScope).Scan(&previous, &lastReserved)
	if err != nil {
		return nil, err
	}
	if now.UnixNano() <= previous {
		return nil, nil
	}
	saved, err := t.savedListRows(platform, account, scope)
	if err != nil {
		return nil, err
	}
	known, err := t.knownListThreads(platform, account, target)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var reviews, fresh []AutoReplyListReview
	var probe AutoReplyListObservation
	var probeRow autoReplyListRow
	reserveTurn := previous != 0 && lastReserved != previous && now.Truncate(time.Minute).After(time.Unix(0, previous).Truncate(time.Minute))
	for _, o := range items {
		if target != "" && o.ThreadID != target {
			continue
		}
		if strings.TrimSpace(o.ThreadID) == "" || o.Signature == "" || seen[o.ThreadID] {
			return nil, fmt.Errorf("联系人列表缺少稳定身份或出现重复，未推进扫描时间")
		}
		seen[o.ThreadID] = true
		key := listDigest(o.ThreadID)
		sig := listDigest(o.Signature)
		preview := listPreviewDigest(o.PreviewDigest)
		old, exists := saved[key]
		r := old
		changed := exists && (old.signature != sig || old.preview != preview)
		if !exists || changed {
			r.signature, r.preview, r.witness = sig, preview, ""
			r.revision = max(now.UnixNano(), old.revision+1)
			if !exists {
				r.checked = now.UnixNano()
			}
			newThread := !exists && previous != 0
			if newThread && !known[o.ThreadID] {
				// 删除时列表行也会被清理；原来的联系人仍在平台上，不得直接认作新出现的消息。
				wasDeleted, lookupErr := t.HasPurgedKey("conversations", "conversation-"+listDigest(platform+"|"+account+"|"+o.ThreadID))
				if lookupErr != nil {
					return nil, lookupErr
				}
				if wasDeleted {
					newThread = false
				}
			}
			recent := o.ActivityTime != nil && !o.ActivityTime.Before(now.Truncate(time.Minute)) && !o.ActivityTime.After(now)
			if changed || newThread || known[o.ThreadID] || recent {
				r.pending = true
				if !old.pending {
					r.after = previous
					if known[o.ThreadID] {
						r.after = 0
					} else if previous == 0 {
						r.after = now.UnixNano()
					}
				}
				if newThread || (exists && old.preview != "" && preview != old.preview) {
					r.witness = preview
				}
				if old.pending && old.preview == preview {
					r.witness = old.witness
				}
			}
			if err := t.saveListRow(platform, account, scope, key, r); err != nil {
				return nil, err
			}
		}
		// 已登记的核对也可能被本地恢复任务挤到下一轮，保留名额必须覆盖它。
		if known[o.ThreadID] && (!r.pending || reserveTurn) && now.Sub(time.Unix(0, r.checked)) >= time.Minute &&
			(probe.ThreadID == "" || r.checked < probeRow.checked) {
			probe, probeRow = o, r
		}
		if r.pending {
			v := listReview(o, r, binding)
			if old.pending {
				reviews = append(reviews, v)
			} else {
				fresh = append(fresh, v)
			}
		}
	}
	// 已经挂起的核对先于本次刚变化的行。忙碌列表跨分钟时给静默线程一个名额。
	reviews = append(reviews, fresh...)
	scopesToUpdate := []string{scope}
	if probe.ThreadID != "" && (len(reviews) == 0 || reserveTurn) {
		wasPending := probeRow.pending
		if !wasPending {
			probeRow.pending, probeRow.after, probeRow.witness = true, 0, ""
			probeRow.revision = max(now.UnixNano(), probeRow.revision+1)
		}
		if reserveTurn {
			// 已给本行一次优先机会。失败仍保留原待办，同时让下个名额轮到其他久未核对的人。
			probeRow.checked = now.UnixNano()
			scopesToUpdate = append(scopesToUpdate, rotationScope)
		}
		if err = t.saveListRow(platform, account, scope, listDigest(probe.ThreadID), probeRow); err != nil {
			return nil, err
		}
		v := listReview(probe, probeRow, binding)
		v.ReserveTurn = reserveTurn
		if wasPending {
			reviews = slices.DeleteFunc(reviews, func(item AutoReplyListReview) bool { return item.ThreadID == probe.ThreadID })
		}
		reviews = slices.Insert(reviews, 0, v)
	}
	for _, key := range scopesToUpdate {
		if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO auto_reply_list_scopes(owner_id,platform_id,account_id,scope_hash,scanned_ns) VALUES(?,?,?,?,?) ON CONFLICT(owner_id,platform_id,account_id,scope_hash) DO UPDATE SET scanned_ns=excluded.scanned_ns`, t.owner, platform, account, key, now.UnixNano()); err != nil {
			return nil, err
		}
	}
	return reviews, nil
}

// AckAutoReplyList 仅在核对成功后清除观察时的精确版本，新消息或删除后重建的行不会被旧回执确认。
func (t *AutoReplyTx) AckAutoReplyList(platform, account, target string, v AutoReplyListReview) error {
	scope := listDigest(target)
	binding := listDigest(t.owner + "\x00" + platform + "\x00" + account + "\x00" + scope)
	if v.binding != binding || platform == "" || account == "" || v.ThreadID == "" || v.Revision <= 0 || (target != "" && v.ThreadID != target) {
		return fmt.Errorf("列表核对凭据缺少范围或身份")
	}
	_, err := t.tx.ExecContext(t.ctx, `UPDATE auto_reply_list_rows SET pending=0,after_ns=0,witness_digest='',checked_ns=MAX(revision,COALESCE((SELECT scanned_ns FROM auto_reply_list_scopes AS s WHERE s.owner_id=auto_reply_list_rows.owner_id AND s.platform_id=auto_reply_list_rows.platform_id AND s.account_id=auto_reply_list_rows.account_id AND s.scope_hash=auto_reply_list_rows.scope_hash),revision)) WHERE owner_id=? AND platform_id=? AND account_id=? AND scope_hash=? AND thread_hash=? AND signature=? AND revision=?`, t.owner, platform, account, listDigest(target), listDigest(v.ThreadID), listDigest(v.Signature), v.Revision)
	return err
}

// DeleteAutoReplyListThread 删除该平台账号下指定线程在普通及限定模式的快照，不影响已有删除边界。
func (t *AutoReplyTx) DeleteAutoReplyListThread(platform, account, thread string) error {
	scope := listDigest(thread)
	if _, err := t.tx.ExecContext(t.ctx, `DELETE FROM auto_reply_list_scopes WHERE owner_id=? AND platform_id=? AND account_id=? AND scope_hash IN (?,?)`, t.owner, platform, account, scope, "rotation/"+scope); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `DELETE FROM auto_reply_list_rows WHERE owner_id=? AND platform_id=? AND account_id=? AND thread_hash=?`, t.owner, platform, account, listDigest(thread))
	return err
}
