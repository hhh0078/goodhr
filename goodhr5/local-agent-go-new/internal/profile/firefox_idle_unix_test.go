//go:build !windows

// Package profile 文件作用：为合成子进程测试提供与 macOS/Linux Firefox 相同的 fcntl 整文件写锁。
package profile

import "golang.org/x/sys/unix"

// nativeFirefoxLockNameForTest 返回 Unix Firefox 实际使用的锁文件名。
func nativeFirefoxLockNameForTest() string { return ".parentlock" }

// holdFirefoxNativeLockForTest 仅在测试子进程中申请整文件 F_WRLCK，父进程生产检查只使用 F_GETLK。
func holdFirefoxNativeLockForTest(file string) (func(), error) {
	fd, err := unix.Open(file, unix.O_RDWR|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err = unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return func() { _ = unix.Close(fd) }, nil
}
