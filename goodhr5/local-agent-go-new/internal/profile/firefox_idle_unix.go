//go:build !windows

// Package profile 文件作用：在 macOS/Linux 只读查询 Mozilla 的 fcntl 整文件写锁，不使用 flock 或申请实际锁。
package profile

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// assertFirefoxRegularLockIdle 仅用 F_GETLK 查询现有 .parentlock 的整文件 F_WRLCK，不创建或改写文件。
func assertFirefoxRegularLockIdle(lockPath, name string) error {
	if name != ".parentlock" {
		return nil
	}
	fd, err := unix.Open(lockPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if err = unix.Fstat(fd, &info); err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("Firefox 锁文件类型已经变化")
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err = unix.FcntlFlock(uintptr(fd), unix.F_GETLK, &lock); err != nil {
		return err
	}
	if lock.Type != unix.F_UNLCK {
		return fmt.Errorf("Firefox 账号仍被进程占用")
	}
	return nil
}
