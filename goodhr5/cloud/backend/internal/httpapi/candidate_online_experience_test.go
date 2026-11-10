// 本文件验证在线关联的主档经历按明确年月排序，缺日期、空机构及冲突不猜测，同时保留人工查重原行为。
package httpapi

import "testing"

// TestOnlineCanonicalExperiencesAreStrict 对公司和学校使用同一组反例，防止只有一类经历得到严格处理。
func TestOnlineCanonicalExperiencesAreStrict(t *testing.T) {
	cases := []struct {
		name    string
		records [][2]string
		want    string
		year    int
	}{
		{"unordered", [][2]string{{"后机构", "2020-01"}, {"最早机构", "2015-06"}, {"中间机构", "2018-03"}}, "最早机构", 2015},
		{"same-year-months", [][2]string{{"后机构", "2015-11"}, {"最早机构", "2015-02"}}, "最早机构", 2015},
		{"missing-start", [][2]string{{"已知机构", "2015-02"}, {"起点未知机构", ""}}, "", 0},
		{"earliest-empty-name", [][2]string{{"后机构", "2015-01"}, {"", "2010-03"}}, "", 0},
		{"unknown-month-conflict", [][2]string{{"机构甲", "2015"}, {"机构乙", "2015-01"}}, "", 0},
		{"same-month-conflict", [][2]string{{"机构甲", "2015-02"}, {"机构乙", "2015-02"}}, "", 0},
		{"unknown-month-same-institution", [][2]string{{"同机构", "2015"}, {"同机构", "2015-08"}}, "同机构", 2015},
		{"same-month-duplicate", [][2]string{{"同机构", "2015-02"}, {"同机构", "2015-02"}}, "同机构", 2015},
		{"later-empty-name", [][2]string{{"最早机构", "2015-02"}, {"", "2016-01"}}, "最早机构", 2015},
		{"invalid-month", [][2]string{{"已知机构", "2015-02"}, {"错误日期机构", "2018-13"}}, "", 0},
		{"all-empty", nil, "", 0},
	}
	for _, kind := range []string{"company", "school"} {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var candidate PositionCandidate
				for _, row := range tc.records {
					if kind == "company" {
						candidate.WorkExperiences = append(candidate.WorkExperiences, CandidateWorkExperience{CompanyName: row[0], StartYM: row[1]})
					} else {
						candidate.Educations = append(candidate.Educations, CandidateEducation{SchoolName: row[0], StartYM: row[1]})
					}
				}
				profile := onlineProfileFromCandidate(candidate)
				name, year := profile.EarliestCompanyName, profile.EarliestCompanyYear
				if kind == "school" {
					name, year = profile.EarliestSchoolName, profile.EarliestSchoolYear
				}
				if name != tc.want || (tc.year == 0 && year != nil) || (tc.year != 0 && (year == nil || *year != tc.year)) {
					t.Fatalf("起始年月不确定时被猜测或未按年月排序：name=%q year=%v", name, year)
				}
			})
		}
	}
}

// TestOnlineCanonicalStartAcceptsOnlyExplicitYearMonth 验证真实年月格式与年份未知月可解析，文本前缀、区间、错误月份不能被截成年份。
func TestOnlineCanonicalStartAcceptsOnlyExplicitYearMonth(t *testing.T) {
	for raw, want := range map[string]int{"2015": 0, "2015年": 0, "2015-02": 2, "2015/2": 2, "2015.02": 2, "2015年2月": 2} {
		year, month, ok := onlineCanonicalExperienceStart(raw, 2026)
		if !ok || year != 2015 || month != want {
			t.Fatalf("明确年月无法读取：%q %d/%d/%v", raw, year, month, ok)
		}
	}
	for _, raw := range []string{"", "2015-00", "2015-13", "2015.99", "201502", "2015-2020", "2015年至今", "开始于2015-02", "2015-02-99", "2027-01"} {
		if _, _, ok := onlineCanonicalExperienceStart(raw, 2026); ok {
			t.Fatalf("不明确或非法开始年月被接受：%q", raw)
		}
	}
}

// TestOnlineCanonicalStrictnessDoesNotChangeLegacyDuplicate 确保人工查重仍使用历史按年及缺项容错行为，严格规则只属于在线自动关联。
func TestOnlineCanonicalStrictnessDoesNotChangeLegacyDuplicate(t *testing.T) {
	for _, records := range [][][2]string{{{"同年后机构", "2015-11"}, {"同年前机构", "2015-02"}}, {{"同年后机构", "2015"}, {"缺日期机构", ""}}, {{"", "2010-01"}, {"同年后机构", "2015-02"}}} {
		name, year := candidateDuplicateEarliest(records)
		if name != "同年后机构" || year != 2015 {
			t.Fatalf("人工查重旧规则被改动：%q %d", name, year)
		}
	}
}
