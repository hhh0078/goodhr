//go:build windows

// Package files 文件作用：使用 Windows 默认程序打开文件并在资源管理器中显示文件。
package files

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

// openPath 使用 Windows 默认程序打开文件。
func openPath(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	procedure := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	result, _, callErr := procedure.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("open"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(path))),
		0,
		0,
		1,
	)
	if result <= 32 {
		return fmt.Errorf("Windows 打开文件失败：code=%d err=%v", result, callErr)
	}
	return nil
}

// revealPath 在 Windows 资源管理器中选中文件。
func revealPath(ctx context.Context, path string) error {
	command := exec.CommandContext(ctx, "explorer.exe", "/select,"+path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command.Run()
}
