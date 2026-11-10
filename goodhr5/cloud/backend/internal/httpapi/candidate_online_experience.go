// 本文件仅为在线自动关联严格提取主档最早经历；缺失或冲突日期保持未知，不改变人工查重的历史容错规则。
package httpapi

import (
	"regexp"
	"strconv"
	"strings"
)

var onlineCanonicalStartYM = regexp.MustCompile(`^((?:19|20)[0-9]{2})(?:年|[-./年]([0-9]{1,2})月?)?$`)

// onlineCanonicalExperience 保留一条已有经历的明确开始年月，月份为零表示只知年份。
type onlineCanonicalExperience struct {
	name        string
	year, month int
}

// onlineCanonicalExperienceStart 只解析完整开始年份或年月，不从结束日期、任意正文或错误月份中截取年份。
func onlineCanonicalExperienceStart(raw string, currentYear int) (int, int, bool) {
	match := onlineCanonicalStartYM.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) != 3 {
		return 0, 0, false
	}
	year, _ := strconv.Atoi(match[1])
	if year < 1900 || year > currentYear {
		return 0, 0, false
	}
	month := 0
	if match[2] != "" {
		month, _ = strconv.Atoi(match[2])
		if month < 1 || month > 12 {
			return 0, 0, false
		}
	}
	return year, month, true
}

// onlineCanonicalEarliestExperience 先核实每条起始日期，再按年月求最早；无法排除更早记录或并列机构冲突时整体留空。
func onlineCanonicalEarliestExperience(records [][2]string, currentYear int) (string, *int) {
	minimumYear := 0
	var earliest []onlineCanonicalExperience
	for _, record := range records {
		year, month, known := onlineCanonicalExperienceStart(record[1], currentYear)
		if !known {
			return "", nil
		}
		entry := onlineCanonicalExperience{name: strings.TrimSpace(record[0]), year: year, month: month}
		if minimumYear == 0 || year < minimumYear {
			minimumYear, earliest = year, []onlineCanonicalExperience{entry}
		} else if year == minimumYear {
			earliest = append(earliest, entry)
		}
	}
	if len(earliest) == 0 {
		return "", nil
	}
	allMonthsKnown, minimumMonth := true, 13
	for _, entry := range earliest {
		if entry.month == 0 {
			allMonthsKnown = false
		} else if entry.month < minimumMonth {
			minimumMonth = entry.month
		}
	}
	name := ""
	for _, entry := range earliest {
		if allMonthsKnown && entry.month != minimumMonth {
			continue
		}
		if entry.name == "" || name != "" && candidateDuplicateText(name) != candidateDuplicateText(entry.name) {
			return "", nil
		}
		name = entry.name
	}
	return name, &minimumYear
}
