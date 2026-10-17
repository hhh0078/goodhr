//go:build windows

// Package profile 文件作用：使用只读共享句柄核验 Windows Firefox 的独占 parent.lock，不创建或修改文件。
package profile

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// assertFirefoxRegularLockIdle Windows 仅探测现存 parent.lock；任何共享冲突或权限不明均保守阻止更新。
func assertFirefoxRegularLockIdle(lockPath, name string) error {
	if name != "parent.lock" {
		return nil
	}
	value, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(value, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("无法只读打开 Firefox 锁文件：%w", err)
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fmt.Errorf("Firefox 锁文件类型已经变化")
	}
	return nil
}
