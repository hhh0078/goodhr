// Package runtime 文件作用：验证 Node.js 可执行文件能够启动且满足 Worker 版本要求。
package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// checkNodeExecutable 检查指定 Node 文件与主版本号，并限制执行时间以免阻塞安装。
func checkNodeExecutable(ctx context.Context, path string) error {
	if !fileExists(path) {
		return fmt.Errorf("Node.js 暂时没找到：%s", path)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := commandCombinedOutput(checkCtx, nil, path, "--version")
	if err != nil {
		return fmt.Errorf("Node.js 版本读取失败：%w", err)
	}
	versionText := strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
	majorText := strings.SplitN(versionText, ".", 2)[0]
	major, err := strconv.Atoi(majorText)
	if err != nil || major < 22 {
		return fmt.Errorf("Node.js 版本需要 22 或更高，当前是 %s", strings.TrimSpace(string(output)))
	}
	return nil
}
