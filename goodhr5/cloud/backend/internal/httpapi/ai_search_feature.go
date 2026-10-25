// 本文件集中控制暂缓接入的AI助手与向量检索入口，默认关闭，不影响既有岗位筛选AI及钱包聊天接口。
package httpapi

import "os"

// aiSearchEnabled 只接受管理员明确配置的1，防止发布保留代码时意外发起真实检索模型任务。
func aiSearchEnabled() bool { return os.Getenv("GOODHR_AI_SEARCH_ENABLED") == "1" }
