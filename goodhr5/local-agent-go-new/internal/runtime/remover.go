// Package runtime 文件作用：安全删除 GoodHR 自己安装的运行组件，并保留用户账号和业务数据。
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var removableComponents = map[string]string{
	"node_runtime":         "Node 运行环境",
	"cloakbrowser_wrapper": "CloakBrowser 控制组件",
	"cloakbrowser":         "Stable 最新 Chromium",
	"ocr":                  "OCR 组件",
}

// RemoveComponent 停止 Worker 后删除指定的 GoodHR 运行组件。
func (m *Manager) RemoveComponent(component string) error {
	component = strings.TrimSpace(component)
	label, ok := removableComponents[component]
	if !ok {
		return fmt.Errorf("不支持删除这个运行组件：%s", component)
	}
	if !m.installMu.TryLock() {
		return fmt.Errorf("运行组件正在更新中，请等安装结束后再删除")
	}
	defer m.installMu.Unlock()
	if m.worker != nil {
		if err := m.worker.Stop(); err != nil {
			return fmt.Errorf("删除前停止浏览器操作程序失败：%w", err)
		}
	}
	path := m.componentDirectory(component)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("删除%s失败：%w", label, err)
	}
	if err := m.removeVersion(component); err != nil {
		return fmt.Errorf("清理%s版本记录失败：%w", label, err)
	}
	m.configureWorkerEnvironment()
	return nil
}

// componentDirectory 返回 GoodHR 自己管理的组件目录。
func (m *Manager) componentDirectory(component string) string {
	switch component {
	case "node_runtime":
		return filepath.Join(m.runtimeDir, "node")
	case "cloakbrowser_wrapper":
		return filepath.Join(m.workerRoot(), "node_modules")
	case "cloakbrowser":
		return filepath.Join(m.runtimeDir, "cloakbrowser")
	case "ocr":
		return filepath.Join(m.runtimeDir, "ocr")
	default:
		return ""
	}
}

// removeVersion 删除指定组件的本地版本记录。
func (m *Manager) removeVersion(component string) error {
	versions := m.loadVersions()
	delete(versions, component)
	return m.writeVersions(versions)
}
