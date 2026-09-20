//go:build !windows

// Package runtime 文件作用：在非 Windows 平台跳过 Visual C++ 运行库检查。
package runtime

// platformSystemRuntimeStatus 返回非 Windows 平台无需安装 Visual C++ 运行库。
func platformSystemRuntimeStatus() SystemRuntimeStatus {
	return SystemRuntimeStatus{Ready: true, Code: "NOT_REQUIRED", Name: "系统运行库", Message: "当前系统无需 Windows 运行库"}
}
