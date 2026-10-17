// 文件作用：按插件目标绝对路径串行准备与替换代码目录，避免并发更新互删固定备份。
package browserextension

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var destinationLocks sync.Map

// destinationKey 规范化目标目录；Windows 不区分盘符及路径大小写。
func destinationKey(value string) string {
	absolute, err := filepath.Abs(value)
	if err != nil {
		absolute = filepath.Clean(value)
	}
	if runtime.GOOS == "windows" {
		absolute = strings.ToLower(absolute)
	}
	return absolute
}

// lockDestination 在可取消的上下文内取得同路径更新锁，锁覆盖下载、验证和原子替换。
func lockDestination(ctx context.Context, destination string) (func(), error) {
	value, _ := destinationLocks.LoadOrStore(destinationKey(destination), make(chan struct{}, 1))
	gate := value.(chan struct{})
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
