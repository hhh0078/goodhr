// Package common 本文件从平台配置的可见经历行配对机构与起始年月，乱序、缺失和无法区分的并列记录均不猜测。
package common

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var onlineExperienceStart = regexp.MustCompile(`^\s*((?:19|20)[0-9]{2})(.*)$`)
var onlineExperienceYearMonth = regexp.MustCompile(`^\s*((?:19|20)[0-9]{2})[./年-]([0-9]{1,2})(?:月)?(?:$|\s|[-~—–至./])`)
var onlineExperienceLeadingDigits = regexp.MustCompile(`^[0-9]+`)

// onlineExperienceOrigin 保存一条真实经历的机构与已知开始年月，月份为零表示仅有年份。
type onlineExperienceOrigin struct {
	name        string
	year, month int
}

// readConfiguredOnlineExperiences 仅为配置了独立经历行的平台补齐同条机构与年份，不改动未配置平台的原路径。
func readConfiguredOnlineExperiences(ctx context.Context, browser model.Browser, cfg model.Config, profile *model.AutoReplyOnlineProfile) error {
	for _, kind := range []struct {
		name      string
		nameField *string
		yearField **int
	}{{"work", &profile.EarliestCompanyName, &profile.EarliestCompanyYear}, {"school", &profile.EarliestSchoolName, &profile.EarliestSchoolYear}} {
		selector, configured := cfg.Selectors["message.online_profile_"+kind.name+"_item"]
		if !configured {
			continue
		}
		name, hasName := cfg.CandidateFields["online_profile_"+kind.name+"_name"]
		period, hasPeriod := cfg.CandidateFields["online_profile_"+kind.name+"_period"]
		if !hasName || !hasPeriod || len(name.Target.Selectors) == 0 || len(period.Target.Selectors) == 0 {
			return fmt.Errorf("在线经历的机构与时间字段配置不完整，暂不猜测")
		}
		*kind.nameField, *kind.yearField = "", nil
		rows, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, Fields: map[string]contract.SelectorSpec{"name": name, "period": period}, MaxItems: 200, ExpectedMissing: true})
		if err != nil {
			return err
		}
		institution, year := earliestOnlineExperience(rows, time.Now().Year())
		*kind.nameField, *kind.yearField = institution, year
	}
	return nil
}

// earliestOnlineExperience 先核实完整行，再按开始年份和已知月份求最早；最早条缺名称或不能区分时保留未知。
func earliestOnlineExperience(rows []contract.FindAllItem, currentYear int) (string, *int) {
	if len(rows) == 0 || len(rows) > 200 {
		return "", nil
	}
	minimum := 0
	var group []onlineExperienceOrigin
	for index, row := range rows {
		if row.Index != index || row.TotalMatches != len(rows) {
			return "", nil // 隐藏或未读行可能更早，不能把局部列表当完整经历。
		}
		year, month, found := onlineExperiencePeriodStart(row.Fields["period"], currentYear)
		if !found {
			return "", nil
		}
		item := onlineExperienceOrigin{name: strings.TrimSpace(row.Fields["name"]), year: year, month: month}
		if minimum == 0 || year < minimum {
			minimum, group = year, []onlineExperienceOrigin{item}
		} else if year == minimum {
			group = append(group, item)
		}
	}
	allMonthsKnown, minimumMonth := true, 13
	for _, item := range group {
		if item.month == 0 {
			allMonthsKnown = false
		} else if item.month < minimumMonth {
			minimumMonth = item.month
		}
	}
	name := ""
	for _, item := range group {
		if allMonthsKnown && item.month != minimumMonth {
			continue
		}
		if item.name == "" || (name != "" && name != item.name) {
			return "", nil
		}
		name = item.name
	}
	if name == "" {
		return "", nil
	}
	return name, &minimum
}

// onlineExperiencePeriodStart 只解析真实日期区间开头的年或年月，不从正文、工龄或结束时间推算。
func onlineExperiencePeriodStart(raw string, currentYear int) (int, int, bool) {
	match := onlineExperienceStart.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) != 3 {
		return 0, 0, false
	}
	year, _ := strconv.Atoi(match[1])
	if year < 1900 || year > currentYear {
		return 0, 0, false
	}
	if complete := onlineExperienceYearMonth.FindStringSubmatch(raw); len(complete) == 3 {
		month, _ := strconv.Atoi(complete[2])
		if month < 1 || month > 12 {
			return 0, 0, false
		}
		return year, month, true
	}
	tail := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(match[2]), "年"))
	if tail == "" {
		return year, 0, true
	}
	if strings.HasPrefix(tail, ".") || strings.HasPrefix(tail, "/") || onlineExperienceLeadingDigits.MatchString(tail) {
		return 0, 0, false
	}
	if !strings.ContainsRune("-~—–至", []rune(tail)[0]) {
		return 0, 0, false
	}
	// 2020-2024是年份区间；2020-13等不完整或错误月份不能降级成年份。
	if strings.HasPrefix(tail, "-") {
		digits := onlineExperienceLeadingDigits.FindString(strings.TrimSpace(strings.TrimPrefix(tail, "-")))
		if digits != "" && len(digits) != 4 {
			return 0, 0, false
		}
	}
	return year, 0, true
}
