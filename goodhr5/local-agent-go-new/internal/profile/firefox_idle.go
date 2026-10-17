// Package profile 文件作用：只读检查 Firefox Profile 是否仍被浏览器占用，防止断联窗口使用中的插件代码被替换。
package profile

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var firefoxLockNames = []string{"parent.lock", ".parentlock", "lock"}

// AssertFirefoxProfilesIdle 只检查账号根目录及一级普通 Profile，不创建、删除、改写锁文件或登录资料。
func AssertFirefoxProfilesIdle(root string) error {
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("Firefox 账号根目录不正确，请检查本地数据目录")
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Firefox 账号根目录无法安全核验，请先关闭浏览器后重试")
	}
	if err = assertFirefoxProfileIdle(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("Firefox 账号目录暂时读不到，请先关闭浏览器后重试：%w", err)
	}
	for _, entry := range entries {
		if isFirefoxLockName(entry.Name()) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Firefox 账号目录包含链接，暂不更新插件，请检查账号目录")
		}
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		info, err = os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Firefox 账号目录已经变化，请关闭浏览器后重试")
		}
		if err = assertFirefoxProfileIdle(directory); err != nil {
			return err
		}
	}
	return nil
}

// isFirefoxLockName 判断 Mozilla 新旧锁文件名称，避免把符号链接锁误当成 Profile 目录。
func isFirefoxLockName(name string) bool {
	for _, candidate := range firefoxLockNames {
		if name == candidate {
			return true
		}
	}
	return false
}

// assertFirefoxProfileIdle 核验单个 Profile 的系统锁与旧 PID 链接；不确定时保留窗口和代码。
func assertFirefoxProfileIdle(directory string) error {
	for _, name := range firefoxLockNames {
		lockPath := filepath.Join(directory, name)
		info, err := os.Lstat(lockPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("浏览器占用状态暂时无法确认，请先关闭火狐再打开：%w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(lockPath)
			pid, parseErr := firefoxSymlinkPID(target)
			if readErr != nil || parseErr != nil || processAlive(pid) {
				return fmt.Errorf("火狐窗口可能还开着，请先关闭后再打开，登录资料会保留")
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("浏览器锁文件状态不明确，请先关闭火狐后重试")
		}
		if err = assertFirefoxRegularLockIdle(lockPath, name); err != nil {
			return fmt.Errorf("火狐窗口还在占用账号，请先关闭后再打开，登录资料会保留：%w", err)
		}
	}
	return nil
}

// firefoxSymlinkPID 只接受本机 Mozilla 的 IP:PID 或 IP:+PID；远端地址与未知格式均不猜测。
func firefoxSymlinkPID(target string) (int, error) {
	index := strings.LastIndexByte(target, ':')
	if index <= 0 || index == len(target)-1 || strings.TrimSpace(target) != target {
		return 0, fmt.Errorf("Firefox 链接锁格式不正确")
	}
	ip := net.ParseIP(target[:index])
	if ip == nil {
		return 0, fmt.Errorf("Firefox 链接锁地址不正确")
	}
	if !ip.IsLoopback() {
		local := false
		addresses, err := net.InterfaceAddrs()
		if err != nil {
			return 0, err
		}
		for _, address := range addresses {
			value, _, parseErr := net.ParseCIDR(address.String())
			if parseErr == nil && value.Equal(ip) {
				local = true
				break
			}
		}
		if !local {
			return 0, fmt.Errorf("Firefox 链接锁属于其它机器")
		}
	}
	value := strings.TrimPrefix(target[index+1:], "+")
	if value == "" {
		return 0, fmt.Errorf("Firefox 链接锁缺少进程编号")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("Firefox 链接锁进程编号不正确")
		}
	}
	pid, err := strconv.ParseInt(value, 10, 32)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("Firefox 链接锁进程编号不正确")
	}
	return int(pid), nil
}
