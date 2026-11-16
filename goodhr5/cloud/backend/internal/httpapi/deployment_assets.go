// 本文件公开已发布组件的最小清单，供私有部署准备内网资源，不公开账号、密钥或系统原始配置。
package httpapi

import (
	"encoding/json"
	"net/http"
)

// deploymentAsset 仅包含可公开的组件下载元数据。
type deploymentAsset struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Note    string `json:"note,omitempty"`
}

// deploymentAssets 保存按组件和平台区分的已发布兼容版本。
type deploymentAssets struct {
	RuntimeComponents map[string]map[string]deploymentAsset `json:"runtime_components"`
}

// GetDeploymentAssets 返回已发布兼容组件，最新版指当前 GoodHR 验证并发布的版本。
func (s *Server) GetDeploymentAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	item, err := s.systemConfigs.Get("system.onboarding_config")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "组件清单暂时没读到，请稍后再试")
		return
	}
	var manifest deploymentAssets
	if err := json.Unmarshal([]byte(item.ConfigValue), &manifest); err != nil || len(manifest.RuntimeComponents) == 0 {
		writeError(w, http.StatusServiceUnavailable, "组件清单还没准备好")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": manifest})
}
