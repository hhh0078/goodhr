// 本文件定义简历人工维护、查重、多来源与岗位推荐的统一合同和校验规则。
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrCandidateDuplicate              = errors.New("这位候选人可能已经入库，请先查看查重结果")
	ErrCandidateVersion                = errors.New("简历刚刚被更新了，请刷新后再比较修改")
	ErrCandidateRequest                = errors.New("这次请求的编号已用于其他内容，请刷新后重试")
	candidatePhonePattern              = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
	candidateInternationalPhonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
)

// CandidateLibraryScope 表示已通过认证的团队和成员可见范围。
type CandidateLibraryScope struct {
	TenantID, UserEmail, AuthorName string
	IsAdmin                         bool
	IsExtension                     bool
}

// CandidateSource 表示一条明确确认的候选人平台来源。
type CandidateSource struct {
	PlatformID          string `json:"platform_id"`
	PlatformCandidateID string `json:"platform_candidate_id"`
	SourceType          string `json:"source_type"`
	SourceURL           string `json:"source_url"`
}

// CandidateDuplicateMatch 表示可访问简历的查重结果与匹配原因。
type CandidateDuplicateMatch struct {
	Candidate PositionCandidate
	MatchType string
	Reasons   []string
	GroupKeys []string
}

// CandidateDuplicateQuery 表示精确和模糊查重条件。
type CandidateDuplicateQuery struct {
	Phone, Name, PlatformID, PlatformCandidateID, Keyword, ExcludeID string
	Gender, BirthYM                                                  string
	Age                                                              int
	WorkExperiences                                                  []CandidateWorkExperience
	Educations                                                       []CandidateEducation
}

// CandidateRecommendationEvent 表示不可覆盖的人工推荐阶段事件。
type CandidateRecommendationEvent struct {
	ID          string    `json:"id"`
	FromStage   string    `json:"from_stage"`
	ToStage     string    `json:"to_stage"`
	Content     string    `json:"content"`
	AuthorEmail string    `json:"author_email"`
	AuthorName  string    `json:"author_name"`
	CreatedAt   time.Time `json:"created_at"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// CandidateRecommendation 表示候选人与一个岗位的人工关系。
type CandidateRecommendation struct {
	ID           string                         `json:"id"`
	CandidateID  string                         `json:"candidate_id"`
	PositionID   string                         `json:"position_id"`
	PositionName string                         `json:"position_name"`
	Stage        string                         `json:"stage"`
	AuthorEmail  string                         `json:"author_email"`
	AuthorName   string                         `json:"author_name"`
	CreatedAt    time.Time                      `json:"created_at"`
	UpdatedAt    time.Time                      `json:"updated_at"`
	Events       []CandidateRecommendationEvent `json:"events"`
}

// CandidateLibraryStore 提供独立于岗位运行统计的简历人工维护能力。
type CandidateLibraryStore interface {
	SaveLibraryCandidate(scope CandidateLibraryScope, candidateID string, fields map[string]any, version int64, requestKey string) (PositionCandidate, error)
	FindCandidateDuplicates(scope CandidateLibraryScope, query CandidateDuplicateQuery) ([]CandidateDuplicateMatch, bool, error)
	ListCandidateRecommendations(scope CandidateLibraryScope, candidateID string) ([]CandidateRecommendation, error)
	SaveCandidateRecommendation(scope CandidateLibraryScope, candidateID string, position Position, stage, content, requestKey string, occurredAt ...time.Time) (CandidateRecommendation, error)
}

// candidateRequestReceipt 表示已完成操作的内容指纹和结果ID。
type candidateRequestReceipt struct{ Fingerprint, ResultID string }

var candidateTextColumns = []string{"candidate_name", "birth_ym", "phone", "email", "work_region", "work_years", "basic_info", "education_level", "expected_position", "online_status", "personal_description", "work_status", "raw_text"}
var candidateArrayColumns = []string{"work_experiences", "educations", "certificates", "honors", "project_experiences", "colleague_communications"}
var candidateExtraFields = []string{"gender", "age", "current_region", "expected_region", "current_position", "expected_salary_text", "current_salary_text", "tags", "library_status", "ai_summary", "source_type", "avatar_data_url"}

// normalizeCandidatePhone 整理号码，拒绝掩码、虚构长度和无法确认的号码。
func normalizeCandidatePhone(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "", "-", "", "(", "", ")", "", "（", "", "）", "").Replace(value)
	if strings.HasPrefix(value, "0086") {
		value = strings.TrimPrefix(value, "0086")
	}
	if strings.HasPrefix(value, "+86") {
		value = strings.TrimPrefix(value, "+86")
	}
	if len(value) == 13 && strings.HasPrefix(value, "86") && candidatePhonePattern.MatchString(value[2:]) {
		value = value[2:]
	}
	if candidatePhonePattern.MatchString(value) || candidateInternationalPhonePattern.MatchString(value) {
		return value
	}
	return ""
}

// candidateMutationFields 校验用户明确提交的字段，不把省略字段当作清空。
func candidateMutationFields(input map[string]any, creating bool) (map[string]any, error) {
	result := map[string]any{}
	if value, present := input["folder_ids"]; present {
		ids, err := candidateFolderIDs(value)
		if err != nil {
			return nil, err
		}
		result["folder_ids"] = ids
	}
	for _, key := range append(append([]string{}, candidateTextColumns...), candidateExtraFields...) {
		value, present := input[key]
		if !present {
			continue
		}
		if key == "avatar_data_url" {
			avatar, err := normalizeCandidateAvatar(value)
			if err != nil {
				return nil, err
			}
			// 当前不提供删除头像入口，空值不写入字段集合，更新时保留原头像。
			if avatar != "" {
				result[key] = avatar
			}
			continue
		}
		if key == "tags" {
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, errors.New("标签格式没认出来")
			}
			var tags []string
			if json.Unmarshal(raw, &tags) != nil || len(tags) > 30 {
				return nil, errors.New("标签请使用文字数组，最多30个")
			}
			clean := []string{}
			seen := map[string]bool{}
			for _, tag := range tags {
				tag = strings.TrimSpace(tag)
				if tag != "" && !seen[tag] {
					if len([]rune(tag)) > 50 {
						return nil, errors.New("单个标签请控制在50字内")
					}
					clean = append(clean, tag)
					seen[tag] = true
				}
			}
			result[key] = clean
			continue
		}
		if value == nil {
			result[key] = ""
			continue
		}
		text, ok := value.(string)
		if !ok && key == "age" {
			text = fmt.Sprint(value)
			ok = true
		}
		if !ok {
			return nil, fmt.Errorf("%s 请填写文字", key)
		}
		text = strings.TrimSpace(text)
		limit := 20000
		if key == "raw_text" {
			limit = 200000
		}
		if len([]rune(text)) > limit {
			return nil, errors.New("简历内容太长了，请分开整理")
		}
		result[key] = text
	}
	for _, key := range candidateArrayColumns {
		value, present := input[key]
		if !present {
			continue
		}
		if value == nil {
			value = []any{}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, errors.New("经历格式没认出来")
		}
		var list []map[string]any
		if json.Unmarshal(raw, &list) != nil || len(list) > 100 {
			return nil, errors.New("经历请使用对象数组，每类最多100条")
		}
		if len(raw) > 512000 {
			return nil, errors.New("经历内容太长了")
		}
		result[key] = list
	}
	for _, key := range []string{"expected_salary_min", "expected_salary_max"} {
		value, present := input[key]
		if !present {
			continue
		}
		if value == nil || value == "" {
			result[key] = nil
			continue
		}
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 1000000 {
			return nil, errors.New("期望薪资请填写有效数字，单位K")
		}
		result[key] = number
	}
	if name, present := result["candidate_name"]; present && strings.TrimSpace(fmt.Sprint(name)) == "" {
		return nil, errors.New("姓名还没填，我先小声提醒一下")
	}
	if creating && strings.TrimSpace(candidateString(result, "candidate_name")) == "" {
		return nil, errors.New("请先填写候选人姓名")
	}
	if phone, present := result["phone"]; present {
		normal := normalizeCandidatePhone(fmt.Sprint(phone))
		if normal == "" {
			return nil, errors.New("手机号还没认准，请填完整的有效号码")
		}
		result["phone"] = normal
	}
	if creating && candidateString(result, "phone") == "" {
		return nil, errors.New("手机号暂时没认出来，补一下，我再帮你入库")
	}
	if creating && candidateString(result, "source_type") == "" {
		result["source_type"] = "manual"
	}
	if value, present := result["source_type"]; present {
		switch value {
		case "manual", "ai_import", "browser_extension":
		default:
			return nil, errors.New("简历来源没认出来")
		}
	}
	for _, key := range []string{"platform_id", "platform_candidate_id", "source_url"} {
		if value, present := input[key]; present {
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("来源信息格式没认出来")
			}
			text = strings.TrimSpace(text)
			if len(text) > 2048 {
				return nil, errors.New("来源地址太长了")
			}
			result[key] = text
		}
	}
	if strings.EqualFold(candidateString(result, "platform_id"), "liepin_hunter") {
		result["platform_id"] = "hliepin"
	}
	if min, max := result["expected_salary_min"], result["expected_salary_max"]; min != nil && max != nil {
		if min.(float64) > max.(float64) {
			return nil, errors.New("最低薪资不能高于最高薪资")
		}
	}
	if birth := candidateString(result, "birth_ym"); birth != "" {
		if _, err := time.Parse("2006-01", birth); err != nil {
			return nil, errors.New("出生年月请使用 YYYY-MM 格式")
		}
	}
	return result, nil
}

// candidateString 安全读取简历字典中的文字字段。
func candidateString(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return strings.TrimSpace(value)
}

// candidateFingerprint 返回请求指纹，拒绝同一幂等键提交不同内容。
func candidateFingerprint(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// candidateRequestKey 生成成员和操作隔离的内存幂等键。
func candidateRequestKey(scope CandidateLibraryScope, operation, key string) string {
	if key == "" {
		return ""
	}
	return strings.Join([]string{scope.TenantID, scope.UserEmail, operation, key}, "|")
}

// candidateProfileFields 把主体转换为可安全比较和更新的字段集合。
func candidateProfileFields(item PositionCandidate) map[string]any {
	result := map[string]any{"candidate_name": item.CandidateName, "birth_ym": item.BirthYM, "phone": item.Phone, "email": item.Email, "work_region": item.WorkRegion, "work_years": item.WorkYears, "basic_info": item.BasicInfo, "education_level": item.EducationLevel, "expected_position": item.ExpectedPosition, "online_status": item.OnlineStatus, "personal_description": item.PersonalDescription, "work_status": item.WorkStatus, "raw_text": item.RawText, "work_experiences": safeSlice(item.WorkExperiences), "educations": safeSlice(item.Educations), "certificates": safeSlice(item.Certificates), "honors": safeSlice(item.Honors), "project_experiences": safeSlice(item.ProjectExperiences), "colleague_communications": safeSlice(item.Communications), "expected_salary_min": item.ExpectedSalaryMin, "expected_salary_max": item.ExpectedSalaryMax}
	for key, value := range item.LibraryFields {
		result[key] = value
	}
	return result
}

// applyCandidateFields 将经过校验的字段应用到内存主体，省略字段保持原值。
func applyCandidateFields(item *PositionCandidate, fields map[string]any) {
	targets := map[string]*string{"candidate_name": &item.CandidateName, "birth_ym": &item.BirthYM, "phone": &item.Phone, "email": &item.Email, "work_region": &item.WorkRegion, "work_years": &item.WorkYears, "basic_info": &item.BasicInfo, "education_level": &item.EducationLevel, "expected_position": &item.ExpectedPosition, "online_status": &item.OnlineStatus, "personal_description": &item.PersonalDescription, "work_status": &item.WorkStatus, "raw_text": &item.RawText}
	for key, target := range targets {
		if value, ok := fields[key]; ok {
			*target, _ = value.(string)
		}
	}
	for key, target := range map[string]**float64{"expected_salary_min": &item.ExpectedSalaryMin, "expected_salary_max": &item.ExpectedSalaryMax} {
		if value, ok := fields[key]; ok {
			*target = nil
			if number, ok := value.(float64); ok {
				*target = &number
			}
		}
	}
	if item.LibraryFields == nil {
		item.LibraryFields = map[string]any{}
	}
	for _, key := range candidateExtraFields {
		if value, ok := fields[key]; ok {
			item.LibraryFields[key] = value
		}
	}
	arrays := map[string]any{"work_experiences": &item.WorkExperiences, "educations": &item.Educations, "certificates": &item.Certificates, "honors": &item.Honors, "project_experiences": &item.ProjectExperiences, "colleague_communications": &item.Communications}
	for key, target := range arrays {
		if value, ok := fields[key]; ok {
			raw, _ := json.Marshal(value)
			_ = json.Unmarshal(raw, target)
		}
	}
}

// candidateVisible 判断当前成员是否可以访问候选人，团队边界始终生效。
func candidateVisible(scope CandidateLibraryScope, item PositionCandidate) bool {
	return item.TenantID == scope.TenantID && (scope.IsAdmin || strings.EqualFold(item.UserEmail, scope.UserEmail))
}

// candidateDuplicateReasons 对精确和模糊线索分别给出原因，不自动合并。
func candidateDuplicateReasons(item PositionCandidate, query CandidateDuplicateQuery) (string, []string) {
	if item.ID == query.ExcludeID {
		return "", nil
	}
	reasons := []string{}
	if phone := normalizeCandidatePhone(query.Phone); phone != "" && phone == normalizeCandidatePhone(item.Phone) {
		reasons = append(reasons, "手机号相同")
	}
	for _, source := range append(item.Sources, CandidateSource{PlatformID: item.PlatformID, PlatformCandidateID: item.PlatformCandidateID}) {
		if query.PlatformID != "" && query.PlatformCandidateID != "" && source.PlatformID == query.PlatformID && source.PlatformCandidateID == query.PlatformCandidateID {
			reasons = append(reasons, "平台候选人编号相同")
			break
		}
	}
	if len(reasons) > 0 {
		return "exact", reasons
	}
	if query.Name != "" && strings.Contains(strings.ToLower(item.CandidateName), strings.ToLower(query.Name)) {
		reasons = append(reasons, "姓名相近")
	}
	if query.Keyword != "" && candidateContainsKeyword(item, query.Keyword) {
		reasons = append(reasons, "经历关键词相近")
	}
	if len(reasons) > 0 {
		return "possible", reasons
	}
	return "", nil
}

// validRecommendationStage 限制人工推进阶段，避免任意字符串污染记录。
func validRecommendationStage(stage string) bool {
	switch stage {
	case "draft", "recommended", "contacting", "interview", "offer", "hired", "paused":
		return true
	}
	return false
}

// sortedCandidateFieldNames 稳定保存人工字段保护列表。
func sortedCandidateFieldNames(fields map[string]any) []string {
	names := []string{}
	for key := range fields {
		if key != "platform_id" && key != "platform_candidate_id" && key != "source_url" {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names
}

// candidateRecommendationFingerprint 将用户明确填写的发生时间纳入幂等校验，旧省略时间请求继续兼容。
func candidateRecommendationFingerprint(stage, content string, times []time.Time) string {
	fields := []string{stage, content}
	if len(times) > 0 && !times[0].IsZero() {
		fields = append(fields, times[0].UTC().Format(time.RFC3339Nano))
	}
	return candidateFingerprint(fields)
}

// candidateRecommendationTime 返回明确填写的发生时间，省略时采用服务端记录时间。
func candidateRecommendationTime(times []time.Time, now time.Time) time.Time {
	if len(times) > 0 && !times[0].IsZero() {
		return times[0]
	}
	return now
}
