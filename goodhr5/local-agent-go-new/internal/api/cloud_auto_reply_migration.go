// Package api 本文件把已绑定用户的旧自动回复资料打包上云，只在收据与本机快照相符后清理原文件。
package api

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"time"

	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

type migrationFile struct {
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
}
type migrationManifest struct {
	Version     int             `json:"version"`
	Owner       string          `json:"owner"`
	Records     int             `json:"records"`
	StateDigest string          `json:"state_digest"`
	Files       []migrationFile `json:"files"`
}
type migrationReceipt struct {
	Digest     string `json:"digest"`
	Accepted   int    `json:"accepted"`
	Reconciled int    `json:"reconciled"`
	Suppressed int    `json:"suppressed"`
	FileCount  int    `json:"file_count"`
}

// deviceMigrationSlot 同一设备一次只核对一个旧账号；等待者仍可被调用超时取消。
var deviceMigrationSlot = make(chan struct{}, 1)

type cloudMigrationState struct {
	Status  string `json:"status"`
	Step    string `json:"step,omitempty"`
	Records int    `json:"records,omitempty"`
	Files   int    `json:"files,omitempty"`
	Error   string `json:"error,omitempty"`
}

// cloudMigrationProgress 仅读取当前已绑定用户的迁移进度，不泄露上一账号状态或路径。
func (s *Server) cloudMigrationProgress() cloudMigrationState {
	s.extensionMu.Lock()
	user := s.extensionUserID
	s.extensionMu.Unlock()
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()
	if user == "" || s.migrationOwner != user {
		return cloudMigrationState{}
	}
	return s.migrationState
}

// reportMigration 在内存更新当前批次进度；数量来自已完成的实际导出而非预估。
func (s *Server) reportMigration(user string, state cloudMigrationState) {
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()
	s.migrationOwner = user
	s.migrationState = state
}

var migrationFileName = regexp.MustCompile(`^[a-f0-9]{64}(\.[a-z0-9]{1,12})?$`)

// migrationHash 返回小写 SHA256，账号目录与历史附件使用相同的寻址方式。
func migrationHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// prepareMigrationFile 只接受原账号受管的内容寻址附件，旧 Windows 相对路径转为可移植斜线。
func prepareMigrationFile(file migrationFile, owner string) (migrationFile, error) {
	file.RelativePath = strings.ReplaceAll(file.RelativePath, "\\", "/")
	name := path.Base(file.RelativePath)
	if !migrationFileName.MatchString(name) || strings.TrimSuffix(name, path.Ext(name)) != file.SHA256 || file.RelativePath != migrationHash(owner)+"/attachments/"+name || file.SizeBytes <= 0 || file.SizeBytes > cloud.AutoReplyMaxAttachmentBytes {
		return file, fmt.Errorf("旧简历附件的归属、文件名或大小无法核实")
	}
	return file, nil
}

// migrationManifestFor 提取全部旧实体和去重附件清单，计算摘要用于写前比较及云端核验。
func migrationManifestFor(owner string, rows []storage.AutoReplyStateChange) (migrationManifest, error) {
	digest, err := storage.AutoReplyExportDigest(rows)
	m := migrationManifest{Version: 1, Owner: owner, Records: len(rows), StateDigest: digest, Files: []migrationFile{}}
	files := map[string]migrationFile{}
	if err != nil {
		return m, err
	}
	for _, row := range rows {
		if row.Kind != "attachments" {
			continue
		}
		var f migrationFile
		if err = json.Unmarshal(row.Payload, &f); err != nil {
			return m, err
		}
		if f, err = prepareMigrationFile(f, owner); err != nil {
			return m, err
		}
		if old, ok := files[f.RelativePath]; ok && old != f {
			return m, fmt.Errorf("同一份附件登记了不同的大小")
		}
		files[f.RelativePath] = f
	}
	for _, f := range files {
		m.Files = append(m.Files, f)
	}
	slices.SortFunc(m.Files, func(a, b migrationFile) int { return strings.Compare(a.RelativePath, b.RelativePath) })
	return m, nil
}

// copyMigrationFile 在账号受管目录内打开普通文件；拒绝软链接、截断文件或摘要变化。
func copyMigrationFile(root *os.Root, f migrationFile, dst io.Writer) error {
	for _, parent := range []string{path.Dir(path.Dir(f.RelativePath)), path.Dir(f.RelativePath)} {
		info, err := root.Lstat(filepath.FromSlash(parent))
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("附件父目录不是受管目录")
		}
	}
	st, err := root.Lstat(filepath.FromSlash(f.RelativePath))
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() != f.SizeBytes {
		return fmt.Errorf("简历附件大小变化或不是普通文件")
	}
	file, err := root.Open(filepath.FromSlash(f.RelativePath))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(st, info) {
		return fmt.Errorf("简历附件在读取前发生变化")
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(file, f.SizeBytes+1))
	if err != nil {
		return err
	}
	if n != f.SizeBytes || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("旧简历附件与登记的 SHA256 不一致")
	}
	return nil
}

// createMigrationArchive 生成账号私有的临时归档。顺序与内容相同的重试会得到相同收据编号。
func createMigrationArchive(file *os.File, dir string, m migrationManifest, rows []storage.AutoReplyStateChange) (string, error) {
	h := sha256.New()
	archive := zip.NewWriter(io.MultiWriter(file, h))
	item, err := archive.Create("manifest.json")
	if err != nil {
		return "", err
	}
	if err = json.NewEncoder(item).Encode(m); err != nil {
		return "", err
	}
	item, err = archive.Create("entities.json")
	if err != nil {
		return "", err
	}
	if err = json.NewEncoder(item).Encode(rows); err != nil {
		return "", err
	}
	if len(m.Files) > 0 {
		root, err := os.OpenRoot(dir)
		if err != nil {
			return "", err
		}
		defer root.Close()
		for _, f := range m.Files {
			item, err = archive.CreateHeader(&zip.FileHeader{Name: "files/" + f.RelativePath, Method: zip.Store})
			if err != nil {
				return "", err
			}
			if err = copyMigrationFile(root, f, item); err != nil {
				return "", err
			}
		}
	}
	if err = archive.Close(); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	_, err = file.Seek(0, io.SeekStart)
	return hex.EncodeToString(h.Sum(nil)), err
}

// clearArchivedFiles 按已核验的清单清理，磁盘占用或中断时保留队列供下次绑定继续。
func clearArchivedFiles(ctx context.Context, store *storage.Store, dir, owner, backupDir, journal string) error {
	if journal == "" {
		return nil
	}
	var files []migrationFile
	if err := json.Unmarshal([]byte(journal), &files); err != nil {
		return err
	}
	if len(files) > 0 {
		root, err := os.OpenRoot(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if root != nil {
			defer root.Close()
			for _, f := range files {
				if f, err = prepareMigrationFile(f, owner); err != nil {
					return err
				}
				if err = copyMigrationFile(root, f, io.Discard); errors.Is(err, os.ErrNotExist) {
					continue
				} else if err != nil {
					return err
				}
				if err = root.Remove(filepath.FromSlash(f.RelativePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	if err := os.RemoveAll(backupDir); err != nil {
		return err
	}
	return store.FinishAutoReplyFileCleanup(ctx, owner, journal)
}

// migrateCloudAutoReply 在启动云端任务前迁移旧数据。Windows 与两种 Mac 均先关闭归档句柄再删除临时文件。
func (s *Server) migrateCloudAutoReply(ctx context.Context, cred cloud.AgentCredentials) error {
	if s.store == nil {
		return fmt.Errorf("旧资料存储尚未就绪")
	}
	s.extensionMu.Lock()
	user := s.extensionUserID
	bound := user != "" && extensionSecretsEqual(s.extensionToken, cred.Token)
	s.extensionMu.Unlock()
	if !bound {
		return fmt.Errorf("账号已变化，请重试")
	}
	if err := s.migrateCloudReplyBusiness(ctx, cred, user); err != nil {
		return err
	}
	if !s.isMigrationSession(cred.Token, user) {
		return fmt.Errorf("设备登录账号已变化，请重试旧日志清理")
	}
	if err := s.store.RedactLegacyReplyDiagnostics(ctx, lifecycle.RedactLegacyReplyLog); err != nil {
		s.reportMigration(user, cloudMigrationState{Status: "failed", Error: "旧自动回复日志尚未清理，重新绑定设备可重试。"})
		return fmt.Errorf("清理旧自动回复日志：%w", err)
	}
	return nil
}

// migrateCloudReplyBusiness 仅在当前绑定用户的服务器收据已经核对后清理其资料，没有待迁资料时直接返回。
func (s *Server) migrateCloudReplyBusiness(ctx context.Context, cred cloud.AgentCredentials, user string) (resultErr error) {
	expected := "auto_reply:v1:" + migrationHash(user)
	has, err := s.store.HasAutoReplyMigrationData(ctx, expected)
	if err != nil || !has {
		return err
	}
	select {
	case deviceMigrationSlot <- struct{}{}:
		defer func() { <-deviceMigrationSlot }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if !s.isMigrationSession(cred.Token, user) {
		return fmt.Errorf("账号已变化，请重试")
	}
	if has, err = s.store.HasAutoReplyMigrationData(ctx, expected); err != nil || !has {
		return err
	}
	state := cloudMigrationState{Status: "reading", Step: "正在核对旧资料"}
	s.reportMigration(user, state)
	defer func() {
		if resultErr != nil {
			state.Status = "failed"
			state.Error = "旧资料迁移未完成，原件已保留。重新连接设备或点击开始可重试。"
		} else {
			state.Status = "completed"
			state.Step = "旧资料已核实并迁移"
		}
		s.reportMigration(user, state)
	}()
	dir := filepath.Join(s.cfg.DataDir, "auto-reply")
	legacy := cloud.New(s.cfg.CloudURL)
	legacy.EnableLocalAutoReply(s.store, dir)
	owner, err := legacy.AuthenticatedAutoReplyOwner(ctx, cred)
	if err != nil {
		return err
	}
	if owner != expected {
		return fmt.Errorf("服务端确认的账号与当前设备绑定不一致")
	}
	backupDir := filepath.Join(s.cfg.DataDir, ".reply-migration", migrationHash(owner))
	journal, err := s.store.GetSetting(ctx, owner+":cleanup")
	if err != nil {
		return err
	}
	if err = clearArchivedFiles(ctx, s.store, dir, owner, backupDir, journal); err != nil {
		return fmt.Errorf("清理上一批迁移附件：%w", err)
	}
	rows, err := s.store.ExportAutoReplyOwner(ctx, owner)
	if err != nil || len(rows) == 0 {
		return err
	}
	manifest, err := migrationManifestFor(owner, rows)
	if err != nil {
		return err
	}
	state.Records = len(rows)
	state.Files = len(manifest.Files)
	state.Step = "正在打包旧资料与附件"
	s.reportMigration(user, state)
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	backup, err := os.CreateTemp(backupDir, "snapshot-*.zip")
	if err != nil {
		return err
	}
	temporary := backup.Name()
	var digest string
	defer func() {
		backup.Close()
		if resultErr != nil && digest != "" {
			// Windows 可能仍占用上次备份，此时保留新文件的临时名称；下次只上传新核验的文件。
			if renameErr := os.Rename(temporary, filepath.Join(backupDir, digest+".zip")); renameErr != nil && !errors.Is(renameErr, os.ErrNotExist) {
				log.Print("迁移备份已按临时名称保留，重新绑定后可重试")
			}
			return
		}
		os.Remove(temporary)
	}()
	// 使用刚刚核验并回卷的同一文件句柄上传；不读取上一次失败留下的备份。
	digest, err = createMigrationArchive(backup, dir, manifest, rows)
	if err != nil {
		return err
	}
	if !s.isMigrationSession(cred.Token, user) {
		return fmt.Errorf("设备账号已切换，旧资料已保留")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.cfg.CloudURL, "/")+"/api/auto-reply/agent/migration/import", backup)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/zip")
	request.Header.Set("Authorization", "Bearer "+cred.Token)
	request.Header.Set("X-GoodHR-Machine-ID", cred.MachineID)
	request.Header.Set("X-GoodHR-Archive-SHA256", digest)
	client := http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	state.Status = "uploading"
	state.Step = "正在上传并等待云端逐件核验"
	s.reportMigration(user, state)
	res, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("上传旧资料未获确认，原资料已保留：%w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("云端未确认旧资料（HTTP %d），原资料已保留", res.StatusCode)
	}
	var receipt migrationReceipt
	if err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&receipt); err != nil {
		return err
	}
	if receipt.Digest != digest || receipt.Accepted < 0 || receipt.Reconciled < 0 || receipt.Suppressed < 0 || receipt.Accepted+receipt.Reconciled+receipt.Suppressed != manifest.Records || receipt.FileCount != len(manifest.Files) {
		return fmt.Errorf("云端收据与导出数量或摘要不同，本机资料已保留")
	}
	list, err := json.Marshal(manifest.Files)
	if err != nil {
		return err
	}
	if !s.isMigrationSession(cred.Token, user) {
		return fmt.Errorf("账号已切换，迁移收据可在下次绑定时继续核实")
	}
	state.Status = "cleaning"
	state.Step = "云端已确认，正在清理旧资料"
	s.reportMigration(user, state)
	if err = s.store.FinishAutoReplyMigration(ctx, owner, manifest.StateDigest, string(list)); err != nil {
		return err
	}
	backup.Close()
	return clearArchivedFiles(ctx, s.store, dir, owner, backupDir, string(list))
}

// isMigrationSession 核对绑定代际的账号与凭证，切换用户后旧请求不可清理资料。
func (s *Server) isMigrationSession(token, user string) bool {
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	return user != "" && s.extensionUserID == user && extensionSecretsEqual(s.extensionToken, token)
}

// migrateAfterBind 在独立后台调用迁移；重新绑定或开始任务均可继续失败的同一份归档。
func (s *Server) migrateAfterBind(cred cloud.AgentCredentials, user string, generation uint64) {
	if s.store == nil {
		return
	}
	s.extensionMu.Lock()
	valid := generation == s.extensionGeneration && user == s.extensionUserID && extensionSecretsEqual(cred.Token, s.extensionToken)
	s.extensionMu.Unlock()
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	if err := s.migrateCloudAutoReply(ctx, cred); err != nil {
		log.Printf("旧自动回复资料迁移未完成，本机档案已保留；重新绑定或启动时可重试：%v", err)
	}
}
