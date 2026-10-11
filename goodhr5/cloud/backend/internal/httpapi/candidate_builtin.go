// 本文件提供登录后共享的只读示例简历，不写数据库，不参与真实候选人统计或查重。
package httpapi

import (
	"net/http"
	"strings"
	"time"
)

const builtinCandidateID = "00000000-0000-4000-8000-000000000001"
const builtinCandidateNotice = "这份是示例简历，由管理员内置，不可修改或删除。未登录的人看不到您的简历库；真实简历仍按账号和团队权限隔离。示例的年龄、学校、公司和经历均为虚构。"

// builtinCandidate 每次构建独立的虚拟资料，避免共享可变数据或复制到不同团队。
func builtinCandidate() PositionCandidate {
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	return PositionCandidate{
		ID: builtinCandidateID, CandidateName: "瓜瓜", Phone: "17607080935", Email: "1224299352@qq.com",
		BirthYM: "1996-04", WorkRegion: "四川成都", WorkYears: "8年", EducationLevel: "本科", ExpectedPosition: "软件开发", WorkStatus: "在职，看看机会",
		CreatorName: "管理员内置", UserEmail: "管理员内置", CreatedAt: created, UpdatedAt: created,
		PersonalDescription: "这是用于熟悉简历库的虚构示例。擅长软件开发、需求沟通和系统维护，资料仅供演示。",
		LibraryFields:       map[string]any{"current_region": "四川成都", "expected_region": "四川成都", "current_position": "软件开发工程师（示例）", "expected_salary_text": "面议（示例）", "source_type": "builtin", "tags": []string{"内置示例", "只读"}},
		WorkExperiences:     []CandidateWorkExperience{{CompanyName: "示例软件工作室（虚构）", PositionName: "软件开发工程师（示例）", StartYM: "2021-03", Content: "虚构经历：负责示例业务系统的开发、需求整理、测试与维护。"}, {CompanyName: "示例技术团队（虚构）", PositionName: "软件开发助理（示例）", StartYM: "2018-07", EndYM: "2021-02", Content: "虚构经历：参与示例项目功能开发与日常维护。"}},
		Educations:          []CandidateEducation{{SchoolName: "示例计算机学院（虚构）", MajorName: "软件工程（示例）", EducationLevel: "本科", StartYM: "2014-09", EndYM: "2018-06"}},
		ProjectExperiences:  []CandidateProjectExperience{{ProjectName: "示例业务管理系统（虚构）", RoleName: "开发成员（示例）", StartYM: "2023-01", EndYM: "2024-06", Content: "虚构项目：完成业务表单、数据查询与权限模块，练习团队协作与交付。"}},
		Notes:               []CandidateNote{{ID: "builtin-guagua-notice", CandidateID: builtinCandidateID, Content: builtinCandidateNotice, AuthorName: "管理员内置", CreatedAt: created, OccurredAt: created}},
	}
}

// publicBuiltinCandidate 沿用详情合同，额外标明只读身份，不伪装成当前账号的真实资料。
func publicBuiltinCandidate() map[string]any {
	result := publicPositionCandidate(builtinCandidate())
	result["is_builtin"], result["readonly"], result["builtin_notice"] = true, true, builtinCandidateNotice
	result["user_email"] = ""
	return result
}

// handleBuiltinCandidate 在认证后处理预留ID，所有写入和未来子路径均默认拒绝。
func (s *CandidateService) handleBuiltinCandidate(w http.ResponseWriter, r *http.Request, path string) bool {
	if strings.SplitN(path, "/", 2)[0] != builtinCandidateID {
		return false
	}
	if _, ok := s.currentSession(w, r); !ok {
		return true
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusForbidden, "这份是内置示例简历，只能查看，不能修改、删除或关联岗位")
		return true
	}
	switch path {
	case builtinCandidateID:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "candidate": publicBuiltinCandidate()})
	case builtinCandidateID + "/notes":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "notes": publicCandidateNotes(builtinCandidate().Notes)})
	case builtinCandidateID + "/recommendations":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recommendations": []CandidateRecommendation{}})
	default:
		writeError(w, http.StatusNotFound, "示例简历没有这个内容入口")
	}
	return true
}

// hasBuiltinCandidateMutation 拒绝伪造内置标识或通过其它入库入口引用预留身份。
func hasBuiltinCandidateMutation(input map[string]any) bool {
	for _, key := range []string{"is_builtin", "readonly", "builtin_notice"} {
		if _, exists := input[key]; exists {
			return true
		}
	}
	for _, key := range []string{"id", "candidate_id", "platform_candidate_id"} {
		if candidateString(input, key) == builtinCandidateID {
			return true
		}
	}
	return candidateString(input, "source_type") == "builtin"
}

// builtinMatchesQuery 仅沿用资料筛选，真实岗位关联和作者范围仍由存储层限制真实简历。
func builtinMatchesQuery(query PositionCandidateQuery) bool {
	item := builtinCandidate()
	return candidateMatchesFilters(item, query) && (query.Keyword == "" || candidateContainsKeyword(item, query.Keyword))
}

// listWithBuiltin 把虚拟示例放在全部真实简历之后，真实存储数量和既有排序保持不变。
func (s *CandidateService) listWithBuiltin(tenantID string, query PositionCandidateQuery) ([]map[string]any, PositionCandidateListResult, int, error) {
	matched := builtinMatchesQuery(query)
	result, err := s.store.ListPositionCandidates(tenantID, query)
	if err != nil || !matched {
		return publicPositionCandidates(result.Items), result, 0, err
	}
	items := publicPositionCandidates(result.Items)
	start := (result.Page - 1) * result.PageSize
	if start <= result.Total && result.Total < start+result.PageSize {
		items = append(items, publicBuiltinCandidate())
	}
	return items, result, 1, nil
}
