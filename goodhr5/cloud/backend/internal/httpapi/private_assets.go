// 本文件在私有部署首次启动时下载并校验 Windows/Mac 组件，持久保存后通过内网提供；后续启动不依赖公网。
package httpapi

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const privateManifestName = "runtime-manifest.json"

// bundledPrivateRuntimeManifest 随私有分支提供已核验组件清单，公网未提供发现接口时仍可部署。
//
//go:embed private_runtime_manifest.json
var bundledPrivateRuntimeManifest []byte

// PrivateRuntimeLibrary 将 Windows 运行库入口固定到本次已缓存并校验的内网文件。
func (s *Server) PrivateRuntimeLibrary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !privateDeployment() {
		http.NotFound(w, r)
		return
	}
	content, err := os.ReadFile(filepath.Join(privateAssetsDir(), privateManifestName))
	var manifest deploymentAssets
	if err != nil || json.Unmarshal(content, &manifest) != nil {
		http.NotFound(w, r)
		return
	}
	asset := manifest.RuntimeComponents["system_runtime"]["win-x64"]
	if !strings.HasPrefix(asset.URL, "/downloads/private/") {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, asset.URL, http.StatusTemporaryRedirect)
}

// privateAssetsDir 返回明确配置的持久组件目录；单测和普通部署默认不开启下载。
func privateAssetsDir() string {
	return strings.TrimSpace(os.Getenv("GOODHR_PRIVATE_ASSETS_DIR"))
}

// preparePrivateAssets 首次下载全部组件后原子发布清单，断网重启读取已校验缓存。
func preparePrivateAssets(store SystemConfigStore) error {
	if !privateDeployment() || privateAssetsDir() == "" {
		return nil
	}
	directory := privateAssetsDir()
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	manifestPath := filepath.Join(directory, privateManifestName)
	var manifest deploymentAssets
	content, err := os.ReadFile(manifestPath)
	if err == nil && os.Getenv("GOODHR_PRIVATE_REFRESH_ASSETS") != "1" {
		if err := json.Unmarshal(content, &manifest); err != nil {
			return fmt.Errorf("内网组件清单损坏：%w", err)
		}
	} else {
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		manifest, err = downloadPrivateAssets(ctx, directory)
		if err != nil {
			return fmt.Errorf("内网组件准备未完成，保留已有文件；检查联网后重试：%w", err)
		}
		content, err = json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(manifestPath+".tmp", content, 0644); err != nil {
			return err
		}
		if err = os.Rename(manifestPath+".tmp", manifestPath); err != nil {
			return err
		}
	}
	if err := validatePrivateManifest(directory, manifest); err != nil {
		return err
	}
	item, err := store.Get("system.onboarding_config")
	if err != nil {
		return err
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(item.ConfigValue), &value); err != nil || value == nil {
		return fmt.Errorf("私有部署初始化配置无法读取")
	}
	absolute := make(map[string]map[string]deploymentAsset)
	for component, platforms := range manifest.RuntimeComponents {
		absolute[component] = make(map[string]deploymentAsset)
		for platform, asset := range platforms {
			if asset.URL != "" {
				asset.URL = deploymentPublicBaseURL() + asset.URL
			}
			absolute[component][platform] = asset
		}
	}
	value["runtime_components"] = absolute
	value["local_agent_console_url"] = deploymentPublicBaseURL() + "/admin"
	// 不能把绑定公网地址的正式安装包当作私有程序提供；打包后通过独立清单登记。
	value["local_agent"] = []any{}
	if clients, err := os.ReadFile(filepath.Join(directory, "clients.json")); err == nil {
		var releases []map[string]any
		if err := json.Unmarshal(clients, &releases); err != nil {
			return err
		}
		value["local_agent"] = releases
	} else if !os.IsNotExist(err) {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	item.ConfigValue = string(encoded)
	if err = store.Save(item); err != nil {
		return err
	}
	return installPrivateExtensionConfig(store)
}

// downloadPrivateAssets 读取本轮发布元数据，下载三个目标平台的全部组件及 Windows VC++ 运行库。
func downloadPrivateAssets(ctx context.Context, directory string) (deploymentAssets, error) {
	source := strings.TrimRight(strings.TrimSpace(os.Getenv("GOODHR_PRIVATE_ASSET_SOURCE")), "/")
	if source == "" {
		source = "https://goodhr.58it.cn"
	}
	client := &http.Client{Timeout: 15 * time.Minute, CheckRedirect: privateAssetRedirect}
	manifest, err := loadPrivateAssetManifest(ctx, client, source)
	if err != nil {
		return deploymentAssets{}, err
	}
	if err := requiredPrivatePlatforms(manifest); err != nil {
		return deploymentAssets{}, err
	}
	manifest.RuntimeComponents["system_runtime"] = map[string]deploymentAsset{"win-x64": {Version: "vc14", URL: "https://aka.ms/vc14/vc_redist.x64.exe", Note: "Microsoft Visual C++ x64 运行库"}}
	for component, platforms := range manifest.RuntimeComponents {
		for platform, asset := range platforms {
			if asset.URL == "" {
				continue
			}
			if component != "system_runtime" && !validAssetHash(asset.SHA256) {
				return deploymentAssets{}, fmt.Errorf("%s/%s 缺少有效 SHA256", component, platform)
			}
			log.Printf("[内网组件准备] component=%s platform=%s version=%s", component, platform, asset.Version)
			local, err := cachePrivateAsset(ctx, client, directory, asset)
			if err != nil {
				return deploymentAssets{}, fmt.Errorf("%s/%s：%w", component, platform, err)
			}
			platforms[platform] = local
		}
	}
	return manifest, nil
}

// loadPrivateAssetManifest 优先读取已发布清单，发现接口不存在时使用分支随附的已核验清单。
func loadPrivateAssetManifest(ctx context.Context, client *http.Client, source string) (deploymentAssets, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source+"/api/system/deployment-assets", nil)
	if err != nil {
		return deploymentAssets{}, err
	}
	if request.URL.Scheme != "https" || request.URL.User != nil {
		return deploymentAssets{}, fmt.Errorf("公网组件源必须使用 HTTPS")
	}
	response, err := client.Do(request)
	if err != nil {
		return deploymentAssets{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		var manifest deploymentAssets
		if err := json.Unmarshal(bundledPrivateRuntimeManifest, &manifest); err != nil {
			return deploymentAssets{}, err
		}
		if err := requiredPrivatePlatforms(manifest); err != nil {
			return deploymentAssets{}, err
		}
		log.Print("[内网组件准备] 公网未提供发现接口，使用私有分支随附的已核验兼容清单")
		return manifest, nil
	}
	var payload struct {
		Config deploymentAssets `json:"config"`
	}
	if response.StatusCode != http.StatusOK {
		return deploymentAssets{}, fmt.Errorf("读取最新兼容组件失败（%d），请确认组件源已升级", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&payload); err != nil {
		return deploymentAssets{}, err
	}
	return payload.Config, nil
}

// privateAssetRedirect 公网准备阶段仅允许 HTTPS 重定向，禁止降级和 URL 凭证。
func privateAssetRedirect(next *http.Request, via []*http.Request) error {
	if len(via) >= 10 || next.URL.Scheme != "https" || next.URL.User != nil {
		return fmt.Errorf("组件下载重定向不正确")
	}
	return nil
}

// cachePrivateAsset 流式下载并校验后按内容地址保存，失败时删除本次临时文件，不覆盖历史文件。
func cachePrivateAsset(ctx context.Context, client *http.Client, directory string, asset deploymentAsset) (deploymentAsset, error) {
	remote, err := url.Parse(asset.URL)
	if err != nil || remote.Scheme != "https" || remote.Host == "" || remote.User != nil {
		return asset, fmt.Errorf("组件源必须为有效 HTTPS 地址")
	}
	file, err := os.CreateTemp(directory, ".download-*")
	if err != nil {
		return asset, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return asset, err
	}
	response, err := client.Do(request)
	if err != nil {
		return asset, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return asset, fmt.Errorf("组件下载失败（%d）", response.StatusCode)
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, (2<<30)+1))
	if err != nil {
		return asset, err
	}
	if n == 0 || n > 2<<30 {
		return asset, fmt.Errorf("组件为空或超过 2GB 限制")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if asset.SHA256 != "" && !strings.EqualFold(digest, asset.SHA256) {
		return asset, fmt.Errorf("组件 SHA256 不一致，本次下载未启用")
	}
	if err := file.Close(); err != nil {
		return asset, err
	}
	// 保留压缩包后缀，供现有 Windows/Mac 解压流程识别。
	name := digest + "-" + path.Base(remote.Path)
	if strings.ContainsAny(name, "\\\r\n") || len(name) > 240 {
		return asset, fmt.Errorf("组件文件名不正确")
	}
	if err := os.Rename(file.Name(), filepath.Join(directory, name)); err != nil {
		return asset, err
	}
	asset.SHA256 = digest
	asset.URL = "/downloads/private/" + name
	return asset, nil
}

// requiredPrivatePlatforms 防止缺少 Windows、Apple 或 Intel 的核心组件时误报准备完成。
func requiredPrivatePlatforms(manifest deploymentAssets) error {
	for _, component := range []string{"node_runtime", "camoufox"} {
		platforms := manifest.RuntimeComponents[component]
		for _, names := range [][]string{{"win-x64", "win"}, {"darwin-arm64", "mac"}, {"darwin-x64"}} {
			found := false
			for _, name := range names {
				if asset := platforms[name]; asset.URL != "" && asset.Version != "" {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("%s 缺少 %s 组件", component, names[0])
			}
		}
	}
	return nil
}

// validAssetHash 判断发布清单是否包含完整十六进制 SHA256。
func validAssetHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// validatePrivateManifest 每次离线启动复核落盘内容，防止损坏或缺失资源被继续发布。
func validatePrivateManifest(directory string, manifest deploymentAssets) error {
	if err := requiredPrivatePlatforms(manifest); err != nil {
		return err
	}
	for _, platforms := range manifest.RuntimeComponents {
		for _, asset := range platforms {
			if asset.URL == "" {
				continue
			}
			if !strings.HasPrefix(asset.URL, "/downloads/private/") || strings.ContainsAny(asset.URL, "?\\#") || path.Base(asset.URL) == "." || !validAssetHash(asset.SHA256) {
				return fmt.Errorf("内网组件清单包含非本地或无效地址")
			}
			file, err := os.Open(filepath.Join(directory, path.Base(asset.URL)))
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, file)
			file.Close()
			if err != nil || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
				return fmt.Errorf("内网组件校验失败：%s", path.Base(asset.URL))
			}
		}
	}
	return nil
}

// installPrivateExtensionConfig 使用随私有镜像构建的插件，避免客户端安装绑定公网默认地址的代码。
func installPrivateExtensionConfig(store SystemConfigStore) error {
	directory := os.Getenv("GOODHR_PRIVATE_EXTENSION_DIR")
	if directory == "" {
		return nil
	}
	content, err := os.ReadFile(filepath.Join(directory, "checksums.json"))
	if err != nil {
		return err
	}
	var built struct {
		Version  string `json:"version"`
		Packages []struct {
			Browser string `json:"browser"`
			File    string `json:"file"`
			SHA256  string `json:"sha256"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(content, &built); err != nil {
		return err
	}
	release := map[string]any{"version": built.Version, "firefox_signed": false, "note": "企业内网部署插件，请使用本企业服务地址。"}
	for _, item := range built.Packages {
		if strings.HasSuffix(item.File, ".zip") {
			release["url_"+item.Browser] = "/zip/" + item.File
			release["sha256_"+item.Browser] = item.SHA256
		}
	}
	content, err = json.Marshal(release)
	if err != nil {
		return err
	}
	return store.Save(SystemConfig{ConfigKey: browserExtensionConfigKey, ConfigValue: string(content), Enabled: true, Description: "私有部署内网插件版本与下载地址"})
}
