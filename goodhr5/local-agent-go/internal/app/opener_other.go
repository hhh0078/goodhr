// Package app 提供非 Windows 系统打开默认浏览器的实现。
//go:build !windows

package app

import (
	"os/exec"
	goruntime "runtime"
)

// openDefaultBrowser 用系统默认浏览器打开控制台。
// url 为控制台地址。
func openDefaultBrowser(url string) error {
	if goruntime.GOOS == "darwin" {
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}
