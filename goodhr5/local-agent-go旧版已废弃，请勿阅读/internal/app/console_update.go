// Package app 提供本地控制台前端包状态检查和更新接口。
package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/response"
)

// consoleManifest 表示控制台前端包下载清单。
type consoleManifest struct {
	Console consoleAsset `json:"console"`
	Version string       `json:"version"`
	URL     string       `json:"url"`
	SHA256  string       `json:"sha256"`
}

// consoleAsset 表示单个控制台前端包资源。
type consoleAsset struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// handleConsoleStatus 返回本地控制台前端包状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleConsoleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, map[string]any{"console": s.consolePackageStatus()})
}

// handleConsoleUpdate 根据 manifest 下载并安装控制台前端包。
// w 为响应对象，r 为请求对象。
func (s *Server) handleConsoleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	manifestURL := firstNonEmptyString(stringValue(payload["manifest_url"]), s.cfg.ConsoleManifestURL)
	result, err := s.installConsolePackage(r.Context(), manifestURL)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(w, result)
}

// consolePackageStatus 读取本地控制台前端包状态。
// 返回目录、版本和是否已安装。
func (s *Server) consolePackageStatus() map[string]any {
	status := map[string]any{
		"installed":    false,
		"dir":          s.cfg.FrontendDir,
		"manifest_url": s.cfg.ConsoleManifestURL,
	}
	indexPath := filepath.Join(s.cfg.FrontendDir, "index.html")
	if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
		status["installed"] = true
	}
	versionPath := filepath.Join(s.cfg.FrontendDir, "goodhr-console-version.json")
	raw, err := os.ReadFile(versionPath)
	if err == nil {
		version := map[string]any{}
		if json.Unmarshal(raw, &version) == nil {
			status["version"] = version["version"]
			status["url"] = version["url"]
			status["installed_at"] = version["installed_at"]
		}
	}
	return status
}

// installConsolePackage 安装控制台前端包。
// ctx 为请求上下文，manifestURL 为清单地址。
func (s *Server) installConsolePackage(ctx context.Context, manifestURL string) (map[string]any, error) {
	asset, err := fetchConsoleAsset(ctx, manifestURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(asset.URL) == "" {
		return nil, fmt.Errorf("控制台前端包下载地址为空")
	}
	downloadsDir := filepath.Join(s.cfg.DataDir, "console-downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建控制台下载目录失败：%w", err)
	}
	archivePath := filepath.Join(downloadsDir, "goodhr-console.zip")
	if err := downloadConsoleFile(ctx, asset.URL, archivePath); err != nil {
		return nil, err
	}
	if err := verifyConsoleSHA256(archivePath, asset.SHA256); err != nil {
		return nil, err
	}
	tmpDir := filepath.Join(downloadsDir, fmt.Sprintf("extract-%d", time.Now().UnixNano()))
	defer os.RemoveAll(tmpDir)
	if err := unzipConsolePackage(archivePath, tmpDir); err != nil {
		return nil, err
	}
	staticDir, err := findConsoleStaticDir(tmpDir)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(s.cfg.FrontendDir); err != nil {
		return nil, fmt.Errorf("清理旧控制台前端失败：%w", err)
	}
	if err := copyConsoleDir(staticDir, s.cfg.FrontendDir); err != nil {
		return nil, err
	}
	if err := s.writeConsoleVersion(asset); err != nil {
		return nil, err
	}
	return map[string]any{"updated": true, "console": s.consolePackageStatus()}, nil
}

// fetchConsoleAsset 读取控制台前端包清单。
// ctx 为请求上下文，manifestURL 为清单地址。
func fetchConsoleAsset(ctx context.Context, manifestURL string) (consoleAsset, error) {
	manifestURL = strings.TrimSpace(manifestURL)
	if manifestURL == "" {
		return consoleAsset{}, fmt.Errorf("控制台前端包清单地址不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return consoleAsset{}, fmt.Errorf("创建控制台清单请求失败：%w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return consoleAsset{}, fmt.Errorf("读取控制台清单失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return consoleAsset{}, fmt.Errorf("读取控制台清单失败，状态码：%d", resp.StatusCode)
	}
	var manifest consoleManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&manifest); err != nil {
		return consoleAsset{}, fmt.Errorf("控制台清单不是有效 JSON")
	}
	asset := manifest.Console
	if asset.URL == "" {
		asset = consoleAsset{Version: manifest.Version, URL: manifest.URL, SHA256: manifest.SHA256}
	}
	return asset, nil
}

// downloadConsoleFile 下载控制台前端包。
// ctx 为请求上下文，url 为下载地址，target 为保存路径。
func downloadConsoleFile(ctx context.Context, url string, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return fmt.Errorf("创建控制台前端包下载请求失败：%w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载控制台前端包失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("下载控制台前端包失败，状态码：%d", resp.StatusCode)
	}
	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("创建控制台前端包文件失败：%w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("保存控制台前端包失败：%w", err)
	}
	return nil
}

// verifyConsoleSHA256 校验控制台前端包 sha256。
// sha256Value 为空时跳过校验。
func verifyConsoleSHA256(path string, sha256Value string) error {
	sha256Value = strings.TrimSpace(strings.ToLower(sha256Value))
	if sha256Value == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("读取控制台前端包失败：%w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("计算控制台前端包校验值失败：%w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != sha256Value {
		return fmt.Errorf("控制台前端包校验失败")
	}
	return nil
}

// unzipConsolePackage 解压控制台前端 zip 包。
// archivePath 为 zip 文件路径，targetDir 为解压目录。
func unzipConsolePackage(archivePath string, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("控制台前端包必须是 zip 格式")
	}
	defer reader.Close()
	for _, file := range reader.File {
		cleanName := filepath.Clean(file.Name)
		if cleanName == "." || strings.HasPrefix(cleanName, "..") {
			return fmt.Errorf("控制台前端包包含不安全路径")
		}
		target := filepath.Join(targetDir, cleanName)
		if !strings.HasPrefix(target, filepath.Clean(targetDir)+string(os.PathSeparator)) && target != filepath.Clean(targetDir) {
			return fmt.Errorf("控制台前端包包含越界路径")
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("创建控制台目录失败：%w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建控制台文件目录失败：%w", err)
		}
		source, err := file.Open()
		if err != nil {
			return fmt.Errorf("读取控制台压缩包文件失败：%w", err)
		}
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			_ = source.Close()
			return fmt.Errorf("写入控制台文件失败：%w", err)
		}
		_, copyErr := io.Copy(targetFile, source)
		_ = source.Close()
		_ = targetFile.Close()
		if copyErr != nil {
			return fmt.Errorf("解压控制台文件失败：%w", copyErr)
		}
	}
	return nil
}

// findConsoleStaticDir 查找包含 index.html 的控制台目录。
// root 为解压根目录。
func findConsoleStaticDir(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				found = path
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("查找控制台前端入口失败：%w", err)
	}
	if found == "" {
		return "", fmt.Errorf("控制台前端包缺少 index.html")
	}
	return found, nil
}

// copyConsoleDir 复制控制台前端目录。
// sourceDir 为源目录，targetDir 为目标目录。
func copyConsoleDir(sourceDir string, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()
		if _, err := io.Copy(targetFile, source); err != nil {
			return err
		}
		return nil
	})
}

// writeConsoleVersion 写入控制台前端包版本文件。
// asset 为已安装的控制台前端包资源。
func (s *Server) writeConsoleVersion(asset consoleAsset) error {
	raw, err := json.MarshalIndent(map[string]any{
		"version":      asset.Version,
		"url":          asset.URL,
		"sha256":       asset.SHA256,
		"installed_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.cfg.FrontendDir, "goodhr-console-version.json"), raw, 0o644)
}
