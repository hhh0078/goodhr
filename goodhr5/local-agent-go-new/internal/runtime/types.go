// Package runtime 文件作用：定义运行组件状态、安装清单和安装进度的强类型模型。
package runtime

// Status 表示本机 Node、Worker、Camoufox 和 OCR 状态。
type Status struct {
	Version             string                        `json:"version"`
	AgentVersion        string                        `json:"agent_version"`
	DataDir             string                        `json:"data_dir"`
	ExtensionsDir       string                        `json:"extensions_dir"`
	RuntimeDir          string                        `json:"runtime_dir"`
	Platform            string                        `json:"platform"`
	NodeReady           bool                          `json:"node_ready"`
	NodeInstalled       bool                          `json:"node_installed"`
	NodePath            string                        `json:"node_path"`
	WorkerBuilt         bool                          `json:"worker_built"`
	WorkerReady         bool                          `json:"worker_ready"`
	NodeWorkerInstalled bool                          `json:"node_worker_installed"`
	WorkerEntry         string                        `json:"worker_entry"`
	WorkerDependency    string                        `json:"worker_dependency"`
	CamoufoxReady       bool                          `json:"camoufox_ready"`
	CamoufoxInstalled   bool                          `json:"camoufox_installed"`
	CamoufoxPath        string                        `json:"camoufox_path"`
	CamoufoxVersion     string                        `json:"camoufox_version"`
	OCRInstalled        bool                          `json:"ocr_installed"`
	OCRPath             string                        `json:"ocr_path"`
	InstalledVersions   map[string]InstalledComponent `json:"installed_versions"`
	InstallProgress     InstallProgress               `json:"install_progress"`
	SystemRuntime       SystemRuntimeStatus           `json:"system_runtime"`
	NativeInputSafety   NativeInputSafetyStatus       `json:"native_input_safety"`
}

// SystemRuntimeStatus 表示操作系统原生运行库与 Worker 原生模块的检查结果。
type SystemRuntimeStatus struct {
	Required         bool     `json:"required"`
	Ready            bool     `json:"ready"`
	Code             string   `json:"code"`
	Name             string   `json:"name"`
	Message          string   `json:"message"`
	DownloadURL      string   `json:"download_url,omitempty"`
	MissingLibraries []string `json:"missing_libraries,omitempty"`
}

// InstalledComponent 表示本地已安装组件的一条版本记录。
type InstalledComponent struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
	// ExecutablePath 保存相对于运行组件目录的可执行文件路径，支持程序目录整体迁移。
	ExecutablePath string `json:"executable_path,omitempty"`
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
	NodeRuntime map[string]Asset `json:"node_runtime"`
	Camoufox    map[string]Asset `json:"camoufox"`
	OCR         map[string]Asset `json:"ocr"`
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
