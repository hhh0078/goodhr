// Package runtime 文件作用：在 Mac 主程序首次绑定后按云端摘要同步 Camoufox，校验完成前拦住旧 Worker。
package runtime

import (
	"context"
	"debug/macho"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// EnableStartupCamoufoxSync 启用本次 Mac 进程的一次性校验，重复调用不会重新锁住已通过的检查。
func (m *Manager) EnableStartupCamoufoxSync() {
	if goruntime.GOOS != "darwin" {
		return
	}
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	if m.startupEnabled {
		return
	}
	m.startupEnabled, m.startupPending = true, true
	m.setInstallProgress(InstallProgress{Component: "camoufox", Stage: "queued", Message: "正在等账号连接，随后自动检查浏览器修补包"})
}

// StartupCamoufoxSyncPending 返回首次浏览器检查是否仍未通过，未启用的系统保持旧行为。
func (m *Manager) StartupCamoufoxSyncPending() bool {
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	return m.startupEnabled && m.startupPending
}

// BeginStartupCamoufoxSyncCheck 标记本次后台配置读取已开始，清掉上次失败进度但保留门闩和可信目标。
func (m *Manager) BeginStartupCamoufoxSyncCheck() {
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	if !m.startupEnabled || !m.startupPending {
		return
	}
	m.setInstallProgress(InstallProgress{Running: true, Component: "camoufox", Stage: "queued", Message: "正在检查浏览器修补包，请稍等", Percent: 1})
}

// FailStartupCamoufoxSync 记录配置读取等外部失败，保留门闩供下次绑定重试，不覆盖正在安装的进度。
func (m *Manager) FailStartupCamoufoxSync(err error) {
	if err == nil || !m.StartupCamoufoxSyncPending() {
		return
	}
	if !m.installMu.TryLock() {
		return
	}
	defer m.installMu.Unlock()
	if m.StartupCamoufoxSyncPending() {
		m.failStartupCamoufoxSync(err)
	}
}

// failStartupCamoufoxSync 在持有安装锁时记录错误，旧目录和版本记录不因失败而删除。
func (m *Manager) failStartupCamoufoxSync(err error) {
	progress := m.InstallProgress()
	progress.Running, progress.Component, progress.Stage = false, "camoufox", "failed"
	progress.Message = "浏览器修补包还没准备好，请重新检查：" + err.Error()
	m.setInstallProgress(progress)
}

// SyncStartupCamoufox 同步首次选中的 Mac 浏览器包，仅在确需下载时调用安全关闭回调。
// ctx 控制下载与登记前取消；manifest 为云端清单，beforeInstall 负责确认无任务并停止旧浏览器和 Worker。
func (m *Manager) SyncStartupCamoufox(ctx context.Context, manifest Manifest, beforeInstall func(context.Context) error) (err error) {
	if !m.StartupCamoufoxSyncPending() {
		return nil
	}
	if !m.installMu.TryLock() {
		return fmt.Errorf("运行组件正在更新，请等它忙完这一轮")
	}
	defer m.installMu.Unlock()
	if !m.StartupCamoufoxSyncPending() {
		return nil
	}
	defer func() {
		if err != nil {
			m.failStartupCamoufoxSync(err)
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	asset, err := startupCamoufoxAsset(manifest, platformKey())
	if err != nil {
		return err
	}
	m.startupMu.Lock()
	m.startupTarget = asset
	m.startupMu.Unlock()
	m.setInstallProgress(InstallProgress{Running: true, Component: "camoufox", Stage: "check", Message: "正在核对浏览器修补包版本和摘要", Percent: 1})
	if !m.startupCamoufoxMatches(asset) {
		if beforeInstall == nil {
			return fmt.Errorf("还缺少安全关闭旧浏览器的步骤，请重新连接本地程序")
		}
		if err = beforeInstall(ctx); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = m.installAsset(ctx, "camoufox", "Camoufox 浏览器", "camoufox", asset); err != nil {
			return err
		}
	}
	m.ensureCamoufoxInterface()
	m.configureWorkerEnvironment()
	if err = m.completeStartupCamoufoxSync(); err != nil {
		return err
	}
	m.setInstallProgress(InstallProgress{Component: "camoufox", Stage: "installed", Message: "浏览器修补包已就绪，可以继续搬砖了", Percent: 100})
	return nil
}

// startupCamoufoxAsset 按当前 Mac 架构选包并验证必要字段，Intel 不借用旧的 Apple 芯片别名。
func startupCamoufoxAsset(manifest Manifest, platform string) (Asset, error) {
	aliases := []string{}
	switch platform {
	case "darwin-arm64":
		aliases = []string{"darwin-arm64", "mac-arm64", "macos-arm64", "mac", "macos", "darwin"}
	case "darwin-x64":
		aliases = []string{"darwin-x64"}
	default:
		return Asset{}, fmt.Errorf("当前系统 %s 不使用 Mac 浏览器启动更新", platform)
	}
	for _, alias := range aliases {
		if asset, ok := manifest.Camoufox[alias]; ok {
			asset.Version, asset.URL, asset.SHA256 = strings.TrimSpace(asset.Version), strings.TrimSpace(asset.URL), strings.ToLower(strings.TrimSpace(asset.SHA256))
			if asset.Version == "" {
				return Asset{}, fmt.Errorf("浏览器修补包缺少版本号")
			}
			if err := validateAssetURL(asset.URL); err != nil {
				return Asset{}, fmt.Errorf("浏览器修补包地址没准备好：%w", err)
			}
			if err := validateSHA256(asset.SHA256); err != nil {
				return Asset{}, fmt.Errorf("浏览器修补包摘要没准备好：%w", err)
			}
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("浏览器修补包没有当前系统 %s 的下载配置", platform)
}

// startupCamoufoxMatches 同时核对版本、摘要和登记启动文件，不把旧目录兜底或空摘要当成通过。
func (m *Manager) startupCamoufoxMatches(asset Asset) bool {
	current := m.loadVersions()["camoufox"]
	return strings.TrimSpace(current.Version) == strings.TrimSpace(asset.Version) && validateSHA256(asset.SHA256) == nil &&
		strings.EqualFold(strings.TrimSpace(current.SHA256), strings.TrimSpace(asset.SHA256)) && m.reusableInstalledComponent("camoufox")
}

// reusableInstalledComponent 保留普通组件的旧判断，首次更新的浏览器必须使用有效登记路径且匹配本机架构。
func (m *Manager) reusableInstalledComponent(component string) bool {
	if component != "camoufox" || !m.StartupCamoufoxSyncPending() {
		return m.componentInstalled(component)
	}
	path := m.recordedExecutablePath("camoufox")
	return path != "" && filepath.Clean(path) == filepath.Clean(m.CamoufoxPath()) &&
		checkExecutableFile(path, goruntime.GOOS) == nil && checkStartupCamoufoxArchitecture(path, platformKey()) == nil
}

// validateStartupManualManifest 限制手动重试只能安装已经从云端选中的目标，避免旧包把门闩提前打开。
func (m *Manager) validateStartupManualManifest(manifest Manifest) error {
	if !m.StartupCamoufoxSyncPending() {
		return nil
	}
	m.startupMu.Lock()
	target := m.startupTarget
	m.startupMu.Unlock()
	asset, err := startupCamoufoxAsset(manifest, platformKey())
	if err != nil {
		return err
	}
	if target.Version == "" || asset.Version != target.Version || !strings.EqualFold(asset.SHA256, target.SHA256) {
		return fmt.Errorf("浏览器更新目标还没确认，或下载配置已变化，请重新连接账号后再试")
	}
	return nil
}

// completeStartupCamoufoxSync 仅在目标版本、摘要与登记启动文件全部通过后打开首次检查门闩。
func (m *Manager) completeStartupCamoufoxSync() error {
	if !m.StartupCamoufoxSyncPending() {
		return nil
	}
	m.startupMu.Lock()
	target := m.startupTarget
	m.startupMu.Unlock()
	if target.Version == "" || !m.startupCamoufoxMatches(target) {
		return fmt.Errorf("已安装的浏览器还不是本次要求的修补包，请重新检查")
	}
	m.startupMu.Lock()
	m.startupPending = false
	m.startupMu.Unlock()
	return nil
}

// checkStartupCamoufoxArchitecture 只读取 Mach-O 文件头确认本机 CPU，支持通用二进制，不运行浏览器。
func checkStartupCamoufoxArchitecture(path string, platform string) error {
	want := macho.CpuArm64
	if platform == "darwin-x64" {
		want = macho.CpuAmd64
	} else if platform != "darwin-arm64" {
		return fmt.Errorf("未识别的 Mac 架构 %s", platform)
	}
	if universal, err := macho.OpenFat(path); err == nil {
		defer universal.Close()
		for _, arch := range universal.Arches {
			if arch.Cpu == want {
				return nil
			}
		}
		return fmt.Errorf("通用浏览器启动文件缺少 %s 架构", want)
	}
	binary, err := macho.Open(path)
	if err != nil {
		return fmt.Errorf("浏览器启动文件不是有效的 Mac 程序：%w", err)
	}
	defer binary.Close()
	if binary.Cpu != want {
		return fmt.Errorf("浏览器启动文件为 %s，本机需要 %s", binary.Cpu, want)
	}
	return nil
}
