// 本文件负责复用平台核心候选人模型，避免主流程与平台实现字段漂移。
package httpapi

import "goodhr5/cloud/backend/internal/platformcore"

type CandidateSalary = platformcore.CandidateSalary
type CandidateWorkExperience = platformcore.CandidateWorkExperience
type CandidateEducation = platformcore.CandidateEducation
type CandidateProjectExperience = platformcore.CandidateProjectExperience
type CandidateCommunication = platformcore.CandidateCommunication
type CandidateBasicProfile = platformcore.CandidateBasicProfile
type CandidateResumeAttachment = platformcore.CandidateResumeAttachment
type CandidateAIScore = platformcore.CandidateAIScore
type CandidateAIScores = platformcore.CandidateAIScores
type CandidateDetail = platformcore.CandidateDetail
type CandidateRuntime = platformcore.CandidateRuntime
type CandidateTimestamps = platformcore.CandidateTimestamps
type Candidate = platformcore.Candidate
