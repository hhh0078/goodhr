// Package app 提供 Windows 系统打开默认浏览器的原生实现。
//go:build windows

package app

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	shell32ExecuteDLL = syscall.NewLazyDLL("shell32.dll")
	shellExecuteWProc = shell32ExecuteDLL.NewProc("ShellExecuteW")
)

// openDefaultBrowser 用 Windows ShellExecute 打开默认浏览器。
// url 为控制台地址。
func openDefaultBrowser(url string) error {
	ret, _, err := shellExecuteWProc.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("open"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(url))),
		0,
		0,
		1,
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW 打开失败 code=%d err=%v", ret, err)
	}
	return nil
}
