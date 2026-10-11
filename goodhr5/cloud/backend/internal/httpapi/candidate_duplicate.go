// 本文件复用参考插件的无手机号综合查重规则，统一内存、数据库和分组响应，不自动合并相似简历。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// CandidateDuplicateCriteria 保存已清洗的结构化查重条件，便于用户判断为何匹配。
type CandidateDuplicateCriteria struct {
	Keyword             string `json:"keyword"`
	Surname             string `json:"surname"`
	Gender              string `json:"gender"`
	Age                 int    `json:"age"`
	AgeMin              int    `json:"age_min"`
	AgeMax              int    `json:"age_max"`
	EarliestCompany     string `json:"earliest_company"`
	EarliestCompanyYear int    `json:"earliest_company_year"`
	EarliestSchool      string `json:"earliest_school"`
	EarliestSchoolYear  int    `json:"earliest_school_year"`
}

// readCandidateDuplicateQuery 读取平铺简历线索并限制经历数量，兼容旧查重请求。
func readCandidateDuplicateQuery(input map[string]any) (CandidateDuplicateQuery, error) {
	query := CandidateDuplicateQuery{Phone: candidateString(input, "phone"), Name: candidateString(input, "candidate_name"), PlatformID: candidateString(input, "platform_id"), PlatformCandidateID: candidateString(input, "platform_candidate_id"), Keyword: candidateString(input, "keyword"), ExcludeID: candidateString(input, "exclude_id"), Gender: candidateString(input, "gender"), BirthYM: candidateString(input, "birth_ym")}
	if query.PlatformID == "liepin_hunter" {
		query.PlatformID = "hliepin"
	}
	if len(query.Name) > 200 || len(query.Keyword) > 500 || len(query.PlatformCandidateID) > 2048 || len(query.Gender) > 40 || len(query.BirthYM) > 40 {
		return query, errors.New("查重内容太长了，请减少关键词")
	}
	if value, exists := input["age"]; exists && value != nil && value != "" {
		text := fmt.Sprint(value)
		age, err := strconv.Atoi(text)
		if err != nil || !(age >= 1 && age <= 120 || age >= 1900 && age <= time.Now().Year()) {
			return query, errors.New("查重年龄请填写实际年龄或出生年份")
		}
		query.Age = age
	}
	for key, target := range map[string]any{"work_experiences": &query.WorkExperiences, "educations": &query.Educations} {
		if value, exists := input[key]; exists && value != nil {
			raw, err := json.Marshal(value)
			if err != nil || len(raw) > 512000 || json.Unmarshal(raw, target) != nil {
				return query, errors.New("查重经历格式没认出来")
			}
		}
	}
	if len(query.WorkExperiences) > 100 || len(query.Educations) > 100 {
		return query, errors.New("查重经历每类最多100条")
	}
	return query, nil
}

// candidateDuplicateGender 清洗男女性别，未知性别不强行排除潜在重复。
func candidateDuplicateGender(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "male", "男":
		return "male"
	case "female", "女":
		return "female"
	}
	return ""
}

// candidateDuplicateYear 提取合理开始年份，不依赖平台经历数组的排列顺序。
func candidateDuplicateYear(value string) int {
	for index := 0; index+4 <= len(value); index++ {
		year, err := strconv.Atoi(value[index : index+4])
		if err == nil && year >= 1900 && year <= 2100 {
			return year
		}
	}
	return 0
}

// candidateDuplicateAge 将出生年或出生年月换算成年龄，支持历史年龄文字字段。
func candidateDuplicateAge(age int, birth string) int {
	year := time.Now().Year()
	if age >= 1900 && age <= year {
		age = year - age
	}
	if age >= 1 && age <= 120 {
		return age
	}
	if birthYear := candidateDuplicateYear(birth); birthYear > 0 && birthYear <= year && year-birthYear <= 120 {
		return year - birthYear
	}
	return 0
}

// candidateDuplicateEarliest 优先使用最早有效年份，全部缺少年份时沿用第一条有名称的经历。
func candidateDuplicateEarliest(records [][2]string) (string, int) {
	name, year, fallback := "", 0, ""
	for _, record := range records {
		current := strings.TrimSpace(record[0])
		if current == "" {
			continue
		}
		if fallback == "" {
			fallback = current
		}
		if start := candidateDuplicateYear(record[1]); start > 0 && (year == 0 || start < year) {
			name, year = current, start
		}
	}
	if name == "" {
		name = fallback
	}
	return name, year
}

// candidateDuplicateCriteria 生成姓氏、年龄区间和最早经历条件，只在没有有效手机号时启用结构化查重。
func candidateDuplicateCriteria(query CandidateDuplicateQuery) CandidateDuplicateCriteria {
	criteria := CandidateDuplicateCriteria{Keyword: strings.TrimSpace(query.Keyword), Gender: candidateDuplicateGender(query.Gender), Age: candidateDuplicateAge(query.Age, query.BirthYM)}
	for _, letter := range strings.TrimSpace(query.Name) {
		criteria.Surname = string(letter)
		break
	}
	if criteria.Age > 0 {
		criteria.AgeMin = max(1, criteria.Age-2)
		criteria.AgeMax = criteria.Age + 2
	}
	work, schools := [][2]string{}, [][2]string{}
	for _, item := range query.WorkExperiences {
		work = append(work, [2]string{item.CompanyName, item.StartYM})
	}
	for _, item := range query.Educations {
		schools = append(schools, [2]string{item.SchoolName, item.StartYM})
	}
	criteria.EarliestCompany, criteria.EarliestCompanyYear = candidateDuplicateEarliest(work)
	criteria.EarliestSchool, criteria.EarliestSchoolYear = candidateDuplicateEarliest(schools)
	return criteria
}

// candidateDuplicateStructured 判断是否使用参考项目的综合条件；只有姓名或关键词时保持既有搜索兼容。
func candidateDuplicateStructured(query CandidateDuplicateQuery) bool {
	criteria := candidateDuplicateCriteria(query)
	return normalizeCandidatePhone(query.Phone) == "" && (criteria.Gender != "" || criteria.Age > 0 || criteria.EarliestSchool != "" || criteria.EarliestCompany != "")
}

// candidateDuplicateText 清理学校和公司名称中的空白，并忽略大小写。
func candidateDuplicateText(value string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value))
}

// candidateDuplicateStructuredReasons 校验综合或降级组全部条件，返回可以展示给用户的匹配原因。
func candidateDuplicateStructuredReasons(item PositionCandidate, criteria CandidateDuplicateCriteria, group string) ([]string, bool) {
	reasons := []string{}
	if criteria.Surname != "" {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(item.CandidateName)), strings.ToLower(criteria.Surname)) {
			return nil, false
		}
		reasons = append(reasons, "姓氏相同")
	}
	if criteria.Gender != "" {
		gender := candidateDuplicateGender(candidateString(item.LibraryFields, "gender"))
		if gender != "" && gender != criteria.Gender {
			return nil, false
		}
		if gender == "" {
			reasons = append(reasons, "原简历性别未填写")
		} else {
			reasons = append(reasons, "性别相同")
		}
	}
	if criteria.Age > 0 {
		age, _ := strconv.Atoi(candidateString(item.LibraryFields, "age"))
		actual := candidateDuplicateAge(age, item.BirthYM)
		if actual < criteria.AgeMin || actual > criteria.AgeMax {
			return nil, false
		}
		reasons = append(reasons, "年龄相近（±2岁）")
	}
	query := CandidateDuplicateQuery{WorkExperiences: item.WorkExperiences, Educations: item.Educations}
	actual := candidateDuplicateCriteria(query)
	if group != "surname_gender_company" && criteria.EarliestSchool != "" {
		if candidateDuplicateText(actual.EarliestSchool) != candidateDuplicateText(criteria.EarliestSchool) || criteria.EarliestSchoolYear > 0 && actual.EarliestSchoolYear != criteria.EarliestSchoolYear {
			return nil, false
		}
		reason := "最早学校相同"
		if criteria.EarliestSchoolYear > 0 {
			reason += fmt.Sprintf("（%d年入学）", criteria.EarliestSchoolYear)
		}
		reasons = append(reasons, reason)
	}
	if group != "surname_gender_school" && criteria.EarliestCompany != "" {
		if candidateDuplicateText(actual.EarliestCompany) != candidateDuplicateText(criteria.EarliestCompany) || criteria.EarliestCompanyYear > 0 && actual.EarliestCompanyYear != criteria.EarliestCompanyYear {
			return nil, false
		}
		reason := "最早公司相同"
		if criteria.EarliestCompanyYear > 0 {
			reason += fmt.Sprintf("（%d年入职）", criteria.EarliestCompanyYear)
		}
		reasons = append(reasons, reason)
	}
	return reasons, len(reasons) > 0
}

// selectCandidateDuplicateMatches 优先精确线索和严格综合匹配，只有严格组无可见结果时才按学校或公司放宽。
func selectCandidateDuplicateMatches(scope CandidateLibraryScope, query CandidateDuplicateQuery, items []PositionCandidate) ([]CandidateDuplicateMatch, bool) {
	exact, legacy, strict := []CandidateDuplicateMatch{}, []CandidateDuplicateMatch{}, []CandidateDuplicateMatch{}
	hidden := false
	hasPhone := normalizeCandidatePhone(query.Phone) != ""
	criteria := candidateDuplicateCriteria(query)
	structured := candidateDuplicateStructured(query)
	visible := []PositionCandidate{}
	for _, item := range items {
		if item.TenantID != scope.TenantID || item.ID == query.ExcludeID {
			continue
		}
		kind, reasons := candidateDuplicateReasons(item, query)
		if !candidateVisible(scope, item) {
			if kind == "exact" {
				hidden = true
			}
			continue
		}
		if kind == "exact" {
			exact = append(exact, CandidateDuplicateMatch{Candidate: item, MatchType: kind, Reasons: reasons})
			continue
		}
		if hasPhone {
			continue
		}
		if structured && query.Keyword != "" && !candidateContainsKeyword(item, query.Keyword) {
			continue
		}
		visible = append(visible, item)
		if !structured {
			if kind != "" {
				legacy = append(legacy, CandidateDuplicateMatch{Candidate: item, MatchType: kind, Reasons: reasons})
			}
			continue
		}
		if reasons, ok := candidateDuplicateStructuredReasons(item, criteria, "strict"); ok {
			if query.Keyword != "" {
				reasons = append(reasons, "关键词相近："+query.Keyword)
			}
			strict = append(strict, CandidateDuplicateMatch{Candidate: item, MatchType: "possible", Reasons: reasons, GroupKeys: []string{"strict"}})
		}
	}
	matches := append(exact, legacy...)
	if structured {
		if len(strict) > 0 {
			matches = append(matches, strict...)
		} else if criteria.Surname != "" {
			for _, item := range visible {
				match := CandidateDuplicateMatch{Candidate: item, MatchType: "possible", Reasons: []string{}, GroupKeys: []string{}}
				for _, group := range []string{"surname_gender_school", "surname_gender_company"} {
					if group == "surname_gender_school" && criteria.EarliestSchool == "" || group == "surname_gender_company" && criteria.EarliestCompany == "" {
						continue
					}
					if reasons, ok := candidateDuplicateStructuredReasons(item, criteria, group); ok {
						match.GroupKeys = append(match.GroupKeys, group)
						for _, reason := range reasons {
							if !slicesContainsString(match.Reasons, reason) {
								match.Reasons = append(match.Reasons, reason)
							}
						}
					}
				}
				if len(match.GroupKeys) > 0 {
					if query.Keyword != "" {
						match.Reasons = append(match.Reasons, "关键词相近："+query.Keyword)
					}
					matches = append(matches, match)
				}
			}
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].MatchType != matches[j].MatchType {
			return matches[i].MatchType == "exact"
		}
		return matches[i].Candidate.CreatedAt.After(matches[j].Candidate.CreatedAt)
	})
	if len(matches) > 20 {
		matches = matches[:20]
	}
	return matches, hidden
}

// slicesContainsString 判断原因是否已经收录，避免同一候选人命中多个兜底组时重复展示。
func slicesContainsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// candidateDuplicateSQLClauses 生成结构化候选集白名单条件，数据库和内存继续共用最终匹配规则。
func candidateDuplicateSQLClauses(query CandidateDuplicateQuery, args *[]any) []string {
	criteria := candidateDuplicateCriteria(query)
	add := func(value any) string { *args = append(*args, value); return fmt.Sprintf("$%d", len(*args)) }
	base := []string{}
	if criteria.Surname != "" {
		base = append(base, "BTRIM(cp.candidate_name) ILIKE "+add(strings.TrimPrefix(candidateLike(criteria.Surname), "%")))
	}
	if criteria.Gender != "" {
		gender := add(criteria.Gender)
		base = append(base, "(CASE LOWER(BTRIM(COALESCE(cp.library_fields->>'gender',''))) WHEN '男' THEN 'male' WHEN '女' THEN 'female' WHEN 'male' THEN 'male' WHEN 'female' THEN 'female' ELSE '' END IN ('',"+gender+"))")
	}
	if criteria.Age > 0 {
		year := time.Now().Year()
		age := `CASE WHEN COALESCE(cp.library_fields->>'age','') ~ '^[0-9]{1,4}$' THEN (cp.library_fields->>'age')::integer ELSE 0 END`
		normal := fmt.Sprintf(`CASE WHEN (%[1]s) BETWEEN 1900 AND %[2]d THEN %[2]d-(%[1]s) WHEN (%[1]s) BETWEEN 1 AND 120 THEN (%[1]s) ELSE %[2]d-NULLIF(substring(cp.birth_ym from '(19[0-9]{2}|20[0-9]{2}|2100)'), '')::integer END`, age, year)
		base = append(base, "("+normal+") BETWEEN "+add(criteria.AgeMin)+" AND "+add(criteria.AgeMax))
	}
	if query.Keyword != "" {
		base = append(base, `(cp.candidate_name||' '||cp.phone||' '||cp.email||' '||cp.work_region||' '||cp.work_years||' '||cp.basic_info||' '||cp.education_level||' '||cp.expected_position||' '||cp.personal_description||' '||cp.raw_text||' '||cp.work_experiences::text||' '||cp.educations::text||' '||cp.project_experiences::text||' '||COALESCE(cp.library_fields->>'current_position','')) ILIKE `+add(candidateLike(query.Keyword)))
	}
	earliest := func(column, key, name string, year int) string {
		if name == "" {
			return ""
		}
		clause := fmt.Sprintf(`EXISTS(SELECT 1 FROM (SELECT entry.item->>'%s' AS name,NULLIF(substring(entry.item->>'start_ym' from '(19[0-9]{2}|20[0-9]{2}|2100)'), '')::integer AS year FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cp.%s)='array' THEN cp.%s ELSE '[]'::jsonb END) WITH ORDINALITY AS entry(item,position) WHERE BTRIM(COALESCE(entry.item->>'%s',''))<>'' ORDER BY NULLIF(substring(entry.item->>'start_ym' from '(19[0-9]{2}|20[0-9]{2}|2100)'), '')::integer ASC NULLS LAST,entry.position LIMIT 1) earliest WHERE LOWER(regexp_replace(earliest.name,'[[:space:]]+','','g'))=%s`, key, column, column, key, add(candidateDuplicateText(name)))
		if year > 0 {
			clause += " AND earliest.year=" + add(year)
		}
		return clause + ")"
	}
	school := earliest("educations", "school_name", criteria.EarliestSchool, criteria.EarliestSchoolYear)
	company := earliest("work_experiences", "company_name", criteria.EarliestCompany, criteria.EarliestCompanyYear)
	strict := append([]string{}, base...)
	if school != "" {
		strict = append(strict, school)
	}
	if company != "" {
		strict = append(strict, company)
	}
	clauses := []string{}
	if len(strict) > 0 {
		clauses = append(clauses, "("+strings.Join(strict, " AND ")+")")
	}
	if criteria.Surname != "" {
		for _, clause := range []string{school, company} {
			if clause != "" {
				clauses = append(clauses, "("+strings.Join(append(append([]string{}, base...), clause), " AND ")+")")
			}
		}
	}
	return clauses
}
