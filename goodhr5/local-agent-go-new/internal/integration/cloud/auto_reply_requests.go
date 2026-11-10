// Package cloud 本文件明确自动回复结构化简历和接管提醒可发送到云端的字段。
package cloud

// autoReplyCandidateRequest 只包含授权的结构化资料，来源消息、头像地址、原始简历和沟通记录留在本机。
type autoReplyCandidateRequest struct {
	OnlineProfileCandidateID string                       `json:"online_profile_candidate_id,omitempty"`
	OnlineProfileKey         string                       `json:"online_profile_key,omitempty"`
	ReopenProof              *AutoReplyReopenProof        `json:"reopen_proof,omitempty"`
	PositionID               string                       `json:"position_id"`
	PlatformID               string                       `json:"platform_id"`
	PlatformAccountID        string                       `json:"platform_account_id,omitempty"`
	PlatformCandidateID      string                       `json:"platform_candidate_id"`
	CandidateName            string                       `json:"candidate_name"`
	Gender                   string                       `json:"gender,omitempty"`
	BirthYM                  string                       `json:"birth_ym,omitempty"`
	BirthYMPrecision         string                       `json:"birth_ym_precision,omitempty"`
	Phone                    string                       `json:"phone"`
	Email                    string                       `json:"email,omitempty"`
	Wechat                   string                       `json:"wechat,omitempty"`
	WorkRegion               string                       `json:"work_region,omitempty"`
	WorkYears                string                       `json:"work_years,omitempty"`
	ExpectedSalaryMin        *float64                     `json:"expected_salary_min,omitempty"`
	ExpectedSalaryMax        *float64                     `json:"expected_salary_max,omitempty"`
	EducationLevel           string                       `json:"education_level,omitempty"`
	ExpectedPosition         string                       `json:"expected_position,omitempty"`
	OnlineStatus             string                       `json:"online_status,omitempty"`
	PersonalDescription      string                       `json:"personal_description,omitempty"`
	WorkStatus               string                       `json:"work_status,omitempty"`
	WorkExperiences          []CandidateWorkExperience    `json:"work_experiences,omitempty"`
	Educations               []CandidateEducation         `json:"educations,omitempty"`
	Certificates             []CandidateCertificate       `json:"certificates,omitempty"`
	Honors                   []CandidateHonor             `json:"honors,omitempty"`
	ProjectExperiences       []CandidateProjectExperience `json:"project_experiences,omitempty"`
}

// candidateCloudRequest 在发送边界从本机资料取出允许字段，旧版本已保存的待办同样经过此处。
func candidateCloudRequest(in AutoReplyCandidateInput) autoReplyCandidateRequest {
	return autoReplyCandidateRequest{
		OnlineProfileCandidateID: in.OnlineProfileCandidateID, OnlineProfileKey: in.OnlineProfileKey,
		ReopenProof: in.ReopenProof,
		PositionID:  in.PositionID, PlatformID: in.PlatformID, PlatformAccountID: in.PlatformAccountID, PlatformCandidateID: in.PlatformCandidateID,
		CandidateName: in.CandidateName, Gender: in.Gender, BirthYM: in.BirthYM, BirthYMPrecision: in.BirthYMPrecision,
		Phone: in.Phone, Email: in.Email, Wechat: in.Wechat, WorkRegion: in.WorkRegion, WorkYears: in.WorkYears,
		ExpectedSalaryMin: in.ExpectedSalaryMin, ExpectedSalaryMax: in.ExpectedSalaryMax, EducationLevel: in.EducationLevel,
		ExpectedPosition: in.ExpectedPosition, OnlineStatus: in.OnlineStatus, PersonalDescription: in.PersonalDescription, WorkStatus: in.WorkStatus,
		WorkExperiences: in.WorkExperiences, Educations: in.Educations, Certificates: in.Certificates, Honors: in.Honors, ProjectExperiences: in.ProjectExperiences,
	}
}

// autoReplyNoticeRequest 只包含接管提醒的幂等标识和固定摘要，邮件发送状态由云端记录。
type autoReplyNoticeRequest struct {
	ID                string `json:"id"`
	PositionID        string `json:"position_id"`
	BasedOnMessageKey string `json:"based_on_message_key"`
	ReasonKey         string `json:"reason_key"`
	CandidateName     string `json:"candidate_name"`
	Gender            string `json:"gender"`
	PlatformID        string `json:"platform_id"`
	Reason            string `json:"reason"`
}

// autoReplyNoticeID 复用会话、消息和原因的原有组合，使旧待办重放仍使用同一封提醒编号。
func autoReplyNoticeID(in AutoReplyNotification) string {
	return "notice:" + autoReplyDigest(in.ConversationID+"\x00"+in.BasedOnMessageKey+"\x00"+in.ReasonKey)
}

// noticeCloudRequest 按云端契约构造提醒，隐藏聊天正文、原始原因、来源消息编号和本地投递记录。
func noticeCloudRequest(id string, in AutoReplyNotification) autoReplyNoticeRequest {
	return autoReplyNoticeRequest{
		ID: id, PositionID: in.PositionID, BasedOnMessageKey: autoReplyDigest(in.BasedOnMessageKey), ReasonKey: autoReplyDigest(in.ReasonKey),
		CandidateName: in.CandidateName, Gender: in.Gender, PlatformID: in.PlatformID, Reason: "需要人工协助，请在本地运行记录查看详情。",
	}
}
