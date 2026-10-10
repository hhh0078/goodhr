// Package resume_download 文件作用：安全复制下载、按账号文件摘要防重、保存完整简历并通知待核对项。
package resume_download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	resumeimport "goodhr5/local-agent-go-new/internal/flow/resume_import"
)

// process 在一个位置展示复制、解析、查重、入库、通知步骤，AI中断项永不自动重做。
func (m *Manager) process(ctx context.Context, token string, item entry, check func(context.Context) error) error {
	if item.Status == "queued" {
		if err := check(ctx); err != nil {
			return err
		}
		file, hash, err := openDownload(item.Path)
		if err != nil {
			item.Status, item.Message = "failed", err.Error()
			if err = m.update(ctx, item); err != nil {
				return err
			}
			return m.notify(ctx, token, item, check)
		}
		defer file.Close()
		item.Hash = hash
		m.mu.Lock()
		duplicate := false
		for id, previous := range m.entries {
			if id != item.ID && previous.Owner == item.Owner && previous.Hash == hash && previous.TaskID != "" {
				duplicate = true
				break
			}
		}
		m.mu.Unlock()
		if duplicate {
			item.Status = "duplicate_file"
			return m.update(ctx, item)
		}
		if err := m.update(ctx, item); err != nil {
			return err
		}
		task, err := m.imports.CreateDownload(ctx, item.Owner, token, resumeimport.Upload{Name: item.Name, Reader: file, Source: &resumeimport.DownloadSource{PlatformID: item.Platform, SourceURL: item.SourceURL}}, hash, check)
		if err != nil {
			item.Status, item.Message = "failed", "文件没交给AI，请在简历库重新导入"
			if err = m.update(ctx, item); err != nil {
				return err
			}
			return m.notify(ctx, token, item, check)
		}
		item.TaskID, item.Status = task.ID, "parsing"
		if err := m.update(ctx, item); err != nil {
			return err
		}
	}
	if item.Status == "parsing" {
		if err := check(ctx); err != nil {
			return err
		}
		if err := m.imports.AuthorizeDownload(item.Owner, item.TaskID, check); err != nil {
			return err
		}
		task, err := m.imports.Get(item.Owner, token, item.TaskID)
		if err != nil {
			item.Status, item.Message = "needs_review", "本地草稿暂时没读出来，请从简历库重新读取；我没有重复调用AI"
			if err := m.update(ctx, item); err != nil {
				return err
			}
			return m.notify(ctx, token, item, check)
		}
		if len(task.Items) == 0 {
			return fmt.Errorf("下载导入任务暂时没有草稿")
		}
		if task.Status == "cancelled" {
			item.Status, item.Message = "needs_review", "下载导入已暂停，已有草稿仍在；请从简历库继续"
			if err := m.update(ctx, item); err != nil {
				return err
			}
			return m.notify(ctx, token, item, check)
		}
		draft := task.Items[0]
		switch draft.Status {
		case "queued", "parsing":
			return nil
		case "saved":
			item.Status = "saved"
			return m.update(ctx, item)
		case "ready":
			if draft.Candidate.IsResume == nil || !*draft.Candidate.IsResume {
				item.Status, item.Message = "failed", "AI还未确认这是个人简历，请核对草稿"
				break
			}
			if strings.TrimSpace(draft.Candidate.CandidateName) == "" {
				item.Status, item.Message = "needs_review", "候选人姓名没认出来，补齐后就能入库"
				break
			}
			duplicate, err := m.cloud.LibraryCandidateHasDuplicate(ctx, token, draft.Candidate)
			if err != nil {
				item.Status, item.Message = "needs_review", "简历已解析，云端查重暂时没完成；请从草稿核对后保存"
				break
			}
			if duplicate {
				item.Status, item.Message = "needs_review", "这份简历可能已经入库，请先核对重复记录"
				break
			}
			if err := check(ctx); err != nil {
				return err
			}
			fields, err := json.Marshal(draft.Candidate)
			if err != nil {
				return err
			}
			task, err = m.imports.Confirm(ctx, item.Owner, token, item.TaskID, []resumeimport.ConfirmItem{{ID: draft.ID, Candidate: fields}})
			if err != nil {
				// 写入响应不明确时只读取本地收据，不再次发起保存或AI。
				restored, readErr := m.imports.Get(item.Owner, token, item.TaskID)
				if readErr == nil && len(restored.Items) > 0 && restored.Items[0].Status == "saved" {
					item.Status = "saved"
					return m.update(ctx, item)
				}
				item.Status, item.Message = "needs_review", "简历解析完成，入库结果尚未确认；请从草稿继续，已保存的收据会保留"
				break
			}
			if len(task.Items) > 0 && task.Items[0].Status == "saved" {
				item.Status = "saved"
				return m.update(ctx, item)
			}
			item.Status, item.Message = "failed", "简历解析完成，入库尚未完成；请从草稿继续保存"
		case "not_resume":
			item.Status, item.Message = "not_resume", "这份文件不是个人简历，我已跳过入库"
		case "needs_phone":
			item.Status, item.Message = "needs_review", "这份简历缺完整手机号，请在草稿里补齐后入库"
		default:
			item.Status, item.Message = "failed", draft.Error
			if item.Message == "" {
				item.Message = "这份简历没处理完成，请在草稿里继续"
			}
		}
		if err := m.update(ctx, item); err != nil {
			return err
		}
	}
	return m.notify(ctx, token, item, check)
}

// notify 通知失败可安全重试，同一下载只发送一条消息，文件内容仍在本机。
func (m *Manager) notify(ctx context.Context, token string, item entry, check func(context.Context) error) error {
	if item.Notified || item.Status == "saved" || item.Status == "duplicate_file" || item.Status == "disabled" {
		return nil
	}
	if err := check(ctx); err != nil {
		return err
	}
	link := "/admin/resumes"
	if item.TaskID != "" {
		link += "?resume_import_id=" + url.QueryEscape(item.TaskID)
	}
	title := "下载简历等你看一眼"
	if item.Status == "not_resume" {
		title = "这份下载我先跳过了"
	}
	if err := m.cloud.PushResumeMessage(ctx, token, title, item.Name+"："+item.Message, link, "resume-download:"+item.ID); err != nil {
		return err
	}
	item.Notified = true
	return m.update(ctx, item)
}

// openDownload 拒绝符号链接、空文件和超大文件，计算真实文件摘要后把同一个已打开文件交给导入。
func openDownload(path string) (*os.File, string, error) {
	if !filepath.IsAbs(path) {
		return nil, "", fmt.Errorf("下载文件路径没认出来，请手动导入")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > resumeimport.MaxFileBytes {
		return nil, "", fmt.Errorf("下载文件不存在、为空或超过10MB，请手动导入")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("下载文件没读出来，请手动导入")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, resumeimport.MaxFileBytes+1))
	if err != nil || n != info.Size() || n > resumeimport.MaxFileBytes {
		file.Close()
		return nil, "", fmt.Errorf("下载文件仍在变化，请等下载完成后手动导入")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, "", err
	}
	return file, hex.EncodeToString(hash.Sum(nil)), nil
}
