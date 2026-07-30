// Package runtime 文件作用：定义运行组件状态、安装清单和安装进度的强类型模型。
package runtime

// Status 表示本机 Node、Worker、CloakBrowser 和 OCR 状态。
type Status struct {
	Version               string                        `json:"version"`
	AgentVersion          string                        `json:"agent_version"`
	DataDir               string                        `json:"data_dir"`
	ExtensionsDir         string                        `json:"extensions_dir"`
	RuntimeDir            string                        `json:"runtime_dir"`
	NodeReady             bool                          `json:"node_ready"`
	NodeInstalled         bool                          `json:"node_installed"`
	NodePath              string                        `json:"node_path"`
	WorkerBuilt           bool                          `json:"worker_built"`
	WorkerReady           bool                          `json:"worker_ready"`
	NodeWorkerInstalled   bool                          `json:"node_worker_installed"`
	WorkerEntry           string                        `json:"worker_entry"`
	WorkerDependency      string                        `json:"worker_dependency"`
	CloakBrowserReady     bool                          `json:"cloakbrowser_ready"`
	CloakBrowserInstalled bool                          `json:"cloakbrowser_installed"`
	CloakBrowserPath      string                        `json:"cloakbrowser_path"`
	CloakBrowserVersion   string                        `json:"cloakbrowser_version"`
	OCRInstalled          bool                          `json:"ocr_installed"`
	OCRPath               string                        `json:"ocr_path"`
	InstalledVersions     map[string]InstalledComponent `json:"installed_versions"`
	InstallProgress       InstallProgress               `json:"install_progress"`
}

// InstalledComponent 表示本地已安装组件的一条版本记录。
type InstalledComponent struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
}

// InstallProgress 表示控制台轮询的组件安装进度。
type InstallProgress struct {
	Running   bool   `json:"running"`
	Component string `json:"component"`
	Stage     string `json:"stage"`
	Message   string `json:"message"`
	Percent   int    `json:"percent"`
	Received  int64  `json:"received"`
	Total     int64  `json:"total"`
	UpdatedAt string `json:"updated_at"`
}

// Manifest 表示云端下发的运行组件安装清单。
type Manifest struct {
	NodeRuntime  map[string]Asset `json:"node_runtime"`
	CloakBrowser map[string]Asset `json:"cloakbrowser"`
	OCR          map[string]Asset `json:"ocr"`
}

// Asset 表示一个平台上的单个运行组件压缩包。
type Asset struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Note    string `json:"note,omitempty"`
}

// InstallRequest 表示运行组件安装接口参数。
type InstallRequest struct {
	Manifest Manifest `json:"manifest"`
}
