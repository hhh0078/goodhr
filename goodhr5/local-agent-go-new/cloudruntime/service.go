// Package cloudruntime 为云端后端提供现有自动回复编排模块的公共入口，不执行本机浏览器操作。
package cloudruntime

import "goodhr5/local-agent-go-new/internal/cloudserver"

// Service 复用现有编排服务类型，仅导出同进程托管所需的处理和收尾能力。
type Service = cloudserver.Service

// New 创建可由同一后端进程托管的自动回复服务，沿用原有状态、设备命令及发送幂等校验。
// backendURL 为内部事务接口地址，secret 为仅服务器持有的凭证，files 为私有附件绝对目录。
func New(backendURL, secret, files string) (*Service, error) {
	return cloudserver.New(backendURL, secret, files)
}
