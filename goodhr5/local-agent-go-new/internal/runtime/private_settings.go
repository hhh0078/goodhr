// Package runtime 文件作用：保存本机私密 CloakBrowser Key，避免把敏感值写入日志或普通状态接口。
package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// privateSettings 表示只保存在本机且使用 0600 权限的敏感配置。
type privateSettings struct {
	CloakBrowserLicenseKey string `json:"cloakbrowser_license_key"`
}

// SaveCloakBrowserLicenseKey 校验并保存 Key；写入前停止旧 Worker，空值会清除本机 Key。
func (m *Manager) SaveCloakBrowserLicenseKey(value string) error {
	value = strings.TrimSpace(value)
	if value != "" && !validCloakBrowserLicenseKey(value) {
		return fmt.Errorf("CloakBrowser Key 格式不正确，请重新复制完整 Key")
	}
	if m.worker != nil {
		if err := m.worker.Stop(); err != nil {
			return fmt.Errorf("更新 CloakBrowser Key 前停止浏览器操作程序失败：%w", err)
		}
	}
	path := m.privateSettingsPath()
	if value == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清除本机 CloakBrowser Key 失败：%w", err)
		}
		m.setCloakBrowserLicenseKey("")
		m.configureWorkerEnvironment()
		return nil
	}
	content, err := json.MarshalIndent(privateSettings{CloakBrowserLicenseKey: value}, "", "  ")
	if err != nil {
		return fmt.Errorf("编码本机私密配置失败：%w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建本机配置目录失败：%w", err)
	}
	if err = os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("保存本机 CloakBrowser Key 失败：%w", err)
	}
	if err = os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("设置本机私密配置权限失败：%w", err)
	}
	m.setCloakBrowserLicenseKey(value)
	m.configureWorkerEnvironment()
	return nil
}

// CloakBrowserLicenseConfigured 判断本机是否已有可用格式的 Key。
func (m *Manager) CloakBrowserLicenseConfigured() bool {
	return validCloakBrowserLicenseKey(m.cloakBrowserLicenseKey())
}

// cloakBrowserLicenseKey 返回内存中的 Key，仅供子进程环境和安装命令使用。
func (m *Manager) cloakBrowserLicenseKey() string {
	m.settingsMu.RLock()
	defer m.settingsMu.RUnlock()
	return m.licenseKey
}

// setCloakBrowserLicenseKey 更新内存中的 Key，只有磁盘保存成功后才调用。
func (m *Manager) setCloakBrowserLicenseKey(value string) {
	m.settingsMu.Lock()
	m.licenseKey = strings.TrimSpace(value)
	m.settingsMu.Unlock()
}

// loadPrivateSettings 在程序启动时读取本机 Key，读取失败时保持未配置状态。
func (m *Manager) loadPrivateSettings() {
	content, err := os.ReadFile(m.privateSettingsPath())
	if err != nil {
		return
	}
	var settings privateSettings
	if json.Unmarshal(content, &settings) != nil || !validCloakBrowserLicenseKey(strings.TrimSpace(settings.CloakBrowserLicenseKey)) {
		return
	}
	_ = os.Chmod(m.privateSettingsPath(), 0o600)
	m.setCloakBrowserLicenseKey(settings.CloakBrowserLicenseKey)
}

// privateSettingsPath 返回 GoodHR 数据目录中的私密配置文件路径。
func (m *Manager) privateSettingsPath() string {
	return filepath.Join(filepath.Dir(m.runtimeDir), "private-settings.json")
}

// validCloakBrowserLicenseKey 检查 Key 前缀、长度和安全字符范围。
func validCloakBrowserLicenseKey(value string) bool {
	if !strings.HasPrefix(value, "cb_") || len(value) < 16 || len(value) > 256 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}
