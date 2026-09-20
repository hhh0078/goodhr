//go:build windows

// Package runtime 文件作用：检查 Windows x64 Visual C++ 核心运行库文件。
package runtime

// platformSystemRuntimeStatus 返回 Windows 运行库检查结果与官方安装地址。
func platformSystemRuntimeStatus() SystemRuntimeStatus {
	missing := missingVisualCppLibraries(defaultSystemRoot())
	if len(missing) > 0 {
		return SystemRuntimeStatus{
			Required: true, Code: "VC_RUNTIME_MISSING", Name: "Microsoft Visual C++ x64 运行库",
			Message:     "电脑缺少 Windows 运行库：" + joinLibraryNames(missing) + "。请安装或修复后重新检查",
			DownloadURL: visualCppDownloadURL, MissingLibraries: missing,
		}
	}
	return SystemRuntimeStatus{
		Required: true, Ready: true, Code: "READY", Name: "Microsoft Visual C++ x64 运行库",
		Message: "Windows 运行库已可用", DownloadURL: visualCppDownloadURL,
	}
}

// joinLibraryNames 把缺少的 DLL 名称整理成简洁中文提示。
func joinLibraryNames(names []string) string {
	result := ""
	for index, name := range names {
		if index > 0 {
			result += "、"
		}
		result += name
	}
	return result
}
