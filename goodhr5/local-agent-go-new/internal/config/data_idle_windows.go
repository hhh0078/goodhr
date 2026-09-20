// Package config 文件作用：迁移前检查旧数据目录中的浏览器和运行组件是否仍在运行。
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ensureLegacyDataIdle 只读检查进程镜像路径，避免复制仍被浏览器写入的登录数据。
func ensureLegacyDataIdle(root string) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if openErr != nil {
			continue
		}
		buffer := make([]uint16, 32768)
		size := uint32(len(buffer))
		queryErr := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size)
		windows.CloseHandle(handle)
		if queryErr == nil && strings.HasPrefix(strings.ToLower(windows.UTF16ToString(buffer[:size])), strings.ToLower(filepath.Clean(root))+string(filepath.Separator)) {
			return fmt.Errorf("旧版浏览器或组件还在运行，请关闭后重新启动 GoodHR，再迁移数据")
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	return nil
}

// lockDataMigration 用独占句柄防止两个启动实例同时迁移；进程退出后系统自动释放。
func lockDataMigration(source string) (func(), error) {
	path, err := windows.UTF16PtrFromString(filepath.Join(filepath.Dir(source), ".goodhr-migration.lock"))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("另一个 GoodHR 正在准备数据，请稍后再启动：%w", err)
	}
	return func() { windows.CloseHandle(handle) }, nil
}
