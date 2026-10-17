//go:build windows

// Package profile 文件作用：为合成子进程测试提供与 Windows Firefox 相同的独占 parent.lock 句柄。
package profile

import "golang.org/x/sys/windows"

// nativeFirefoxLockNameForTest 返回 Windows Firefox 实际使用的锁文件名。
func nativeFirefoxLockNameForTest() string { return "parent.lock" }

// holdFirefoxNativeLockForTest 在测试子进程中持有已有文件的独占句柄，不创建或截断文件。
func holdFirefoxNativeLockForTest(file string) (func(), error) {
	name, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}
