// Package model 文件作用：定义平台可选的候选人断点能力，流程保存进度，平台只负责只读核对页面编号。
package model

import "context"

// CandidateResumePoint 仅保存平台编号或联合身份摘要，以及名单前缀摘要，不保存姓名、简历正文或页面元素引用。
type CandidateResumePoint struct {
	CandidateID string `json:"candidate_id"`
	PrefixHash  string `json:"prefix_hash"`
}

// CandidateListResumer 由具备可靠公开编号的平台实现；其他平台保持原有启动行为。
type CandidateListResumer interface {
	// RestoreCandidateList 只读核对已保存的位置，成功时更新本次游标；找不到返回false，读取错误继续返回。
	RestoreCandidateList(context.Context, Browser, Config, CandidateResumePoint, *CandidateListCursor) (bool, error)
	// CandidateResumePoint 交付本批已核实的候选人断点，主流程确认处理完成后才允许持久化。
	CandidateResumePoint(Candidate) (CandidateResumePoint, bool)
}

// CandidateListResumeProvider 让流程封装显式交付底层断点能力；不支持的平台返回 nil，不冒充具备恢复能力。
type CandidateListResumeProvider interface {
	// CandidateListResumer 返回底层平台已有的断点对象，不支持时返回 nil。
	CandidateListResumer() CandidateListResumer
}
