// Package model 本文件定义平台只读在线基础资料能力；没有可信入口时明确不可用，不用附件或猜测值代替。
package model

import (
	"context"
	"errors"
)

// ErrAutoReplyOnlineProfileUnavailable 表示平台没有可信的当前候选人在线资料入口，普通问答仍可继续。
var ErrAutoReplyOnlineProfileUnavailable = errors.New("当前平台尚无可核实的在线基础资料入口")

// AutoReplyOnlineProfile 只承载页面明确展示的基础信息，未知年龄和年份用nil，禁止根据岗位或经历猜测。
type AutoReplyOnlineProfile struct {
	Mode                string   `json:"mode,omitempty"`
	Screenshots         [][]byte `json:"screenshots,omitempty"` // 仅当次设备回传与服务器AI识别使用，入库前必须清空，不接受设备文件路径。
	CandidateName       string   `json:"candidate_name"`
	Gender              string   `json:"gender,omitempty"`
	Age                 *int     `json:"age,omitempty"`
	EducationLevel      string   `json:"education_level,omitempty"`
	EarliestCompanyName string   `json:"earliest_company_name,omitempty"`
	EarliestCompanyYear *int     `json:"earliest_company_year,omitempty"`
	EarliestSchoolName  string   `json:"earliest_school_name,omitempty"`
	EarliestSchoolYear  *int     `json:"earliest_school_year,omitempty"`
	RawText             string   `json:"raw_text"`
	SourceKey           string   `json:"source_key"`
}

// AutoReplyOnlineProfileReader 在当前候选人身份范围读取在线资料并恢复聊天；没有可靠入口时返回专用不可用错误。
type AutoReplyOnlineProfileReader interface {
	ReadAutoReplyOnlineProfile(context.Context, Browser, Config, AutoReplyConversationSnapshot) (AutoReplyOnlineProfile, error)
}
