// Package common 本文件验证在线经历按真实起始年月配对机构，乱序、缺项和并列歧义不能生成错误匹配证据。
package common

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// onlineExperienceRows 构造保留完整列表序号及总数的可见经历行。
func onlineExperienceRows(pairs ...[2]string) []contract.FindAllItem {
	rows := make([]contract.FindAllItem, len(pairs))
	for index, pair := range pairs {
		rows[index] = contract.FindAllItem{Index: index, TotalMatches: len(pairs), Fields: map[string]string{"name": pair[0], "period": pair[1]}}
	}
	return rows
}

// TestOnlineExperienceSelectsActualEarliestMonth 验证DOM倒序、同年不同月、单年份及重复同机构均按证据求最早。
func TestOnlineExperienceSelectsActualEarliestMonth(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []contract.FindAllItem
		want string
		year int
	}{
		{"倒序工作", onlineExperienceRows([2]string{"后来的公司", "2023.09 - 至今"}, [2]string{"最早公司", "2017.02 - 2020.01"}, [2]string{"中间公司", "2020.02 - 2023.08"}), "最早公司", 2017},
		{"同年不同月", onlineExperienceRows([2]string{"下半年公司", "2017.11 - 至今"}, [2]string{"上半年公司", "2017.03 - 2017.10"}), "上半年公司", 2017},
		{"倒序学校", onlineExperienceRows([2]string{"后续学校", "2020/09 - 2023/06"}, [2]string{"最早学校", "2015年09月 - 2018年06月"}), "最早学校", 2015},
		{"单条仅年份", onlineExperienceRows([2]string{"最早公司", "2017 - 2020"}), "最早公司", 2017},
		{"同机构未知月", onlineExperienceRows([2]string{"同一机构", "2017 - 2018"}, [2]string{"同一机构", "2017.03 - 2018.06"}), "同一机构", 2017},
		{"标准横线年月", onlineExperienceRows([2]string{"最早公司", "2017-02 - 2020-01"}), "最早公司", 2017},
	} {
		t.Run(test.name, func(t *testing.T) {
			name, year := earliestOnlineExperience(test.rows, 2026)
			if name != test.want || year == nil || *year != test.year {
				t.Fatalf("未使用真实最早年月：机构=%s 年份=%v", name, year)
			}
		})
	}
}

// TestOnlineExperienceKeepsMissingAndConflictsUnknown 验证最早缺名称、日期缺失、同月冲突及不完整窗口不能跳过后猜测。
func TestOnlineExperienceKeepsMissingAndConflictsUnknown(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []contract.FindAllItem
	}{
		{"空列表", nil},
		{"最早缺机构", onlineExperienceRows([2]string{"", "2015.01 - 2016.01"}, [2]string{"第二家公司", "2017.01 - 2020.01"})},
		{"日期缺失", onlineExperienceRows([2]string{"未知起点", ""}, [2]string{"已知起点", "2017.01 - 至今"})},
		{"同月冲突", onlineExperienceRows([2]string{"甲公司", "2017.03 - 2019.01"}, [2]string{"乙公司", "2017.03 - 2020.01"})},
		{"同年月份不全", onlineExperienceRows([2]string{"甲公司", "2017 - 2019"}, [2]string{"乙公司", "2017.03 - 2020.01"})},
		{"错误月份", onlineExperienceRows([2]string{"甲公司", "2017.13 - 2019.01"})},
		{"未来开始时间", onlineExperienceRows([2]string{"甲公司", "2027.03 - 至今"})},
		{"日期不在开头", onlineExperienceRows([2]string{"甲公司", "共3年，2017.03 - 2020.01"})},
	} {
		t.Run(test.name, func(t *testing.T) {
			name, year := earliestOnlineExperience(test.rows, 2026)
			if name != "" || year != nil {
				t.Fatal("缺项或冲突不应跳过较早记录猜测第二条")
			}
		})
	}
	rows := onlineExperienceRows([2]string{"唯一已读公司", "2017.03 - 至今"})
	rows[0].TotalMatches = 2
	if name, year := earliestOnlineExperience(rows, 2026); name != "" || year != nil {
		t.Fatal("未读取的经历可能更早，不能用部分窗口生成身份匹配字段")
	}
}

// experienceProfileBrowser 提供当前在线面板的独立头部和经历列表，不混入其它候选人或整页文字。
type experienceProfileBrowser struct {
	*onlineProfileTestBrowser
	education string
}

// Read 返回平台明确配置的年龄和完整教育行，验证仅提取学历词而非整行覆盖。
func (b *experienceProfileBrowser) Read(ctx context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	switch request.Selector.Description {
	case "header-age":
		return contract.ReadResult{Value: "28 岁"}, nil
	case "header-education":
		return contract.ReadResult{Value: b.education}, nil
	}
	return b.onlineProfileTestBrowser.Read(ctx, request)
}

// FindAll 仅对配置的工作与教育行返回真实字段，其余沿用面板是否可见的检查。
func (b *experienceProfileBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	for _, key := range []string{"name", "period"} {
		if _, configured := request.Fields[key]; !configured && (request.Selector.Description == "work-items" || request.Selector.Description == "school-items") {
			return nil, nil
		}
	}
	switch request.Selector.Description {
	case "work-items":
		return onlineExperienceRows([2]string{"近期公司", "2021.06 - 至今"}, [2]string{"最早公司", "2017.03 - 2021.05"}), nil
	case "school-items":
		return onlineExperienceRows([2]string{"最早学校", "2014.09 - 2017.06"}), nil
	}
	return b.onlineProfileTestBrowser.FindAll(ctx, request)
}

// TestOnlineProfileReadsScopedExperienceAndHeader 验证头部年龄学历与同条最早机构年份能一起进入DOM资料，学历歧义保持空。
func TestOnlineProfileReadsScopedExperienceAndHeader(t *testing.T) {
	for _, education := range []string{"示例学院 · 软件工程 · 大专 · 统招", "本科 硕士"} {
		original, cfg, snapshot := onlineProfileFixture("liepin")
		b := &experienceProfileBrowser{onlineProfileTestBrowser: original, education: education}
		cfg.CandidateFields = map[string]contract.SelectorSpec{}
		for key, description := range map[string]string{"message.online_profile_age": "header-age", "message.online_profile_education": "header-education", "message.online_profile_work_item": "work-items", "message.online_profile_school_item": "school-items"} {
			cfg.Selectors[key] = contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}, Description: description}
		}
		for _, kind := range []string{"work", "school"} {
			for _, field := range []string{"name", "period"} {
				cfg.CandidateFields["online_profile_"+kind+"_"+field] = contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}}
			}
		}
		verify := func(context.Context) error { return nil }
		profile, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, verify, strings.TrimSpace)
		if err != nil || profile.Age == nil || *profile.Age != 28 || profile.EarliestCompanyName != "最早公司" || profile.EarliestCompanyYear == nil || *profile.EarliestCompanyYear != 2017 || profile.EarliestSchoolName != "最早学校" || profile.EarliestSchoolYear == nil || *profile.EarliestSchoolYear != 2014 {
			t.Fatalf("平台头部或成对经历未读到：%+v err=%v", profile, err)
		}
		want := "大专"
		if education == "本科 硕士" {
			want = ""
		}
		if profile.EducationLevel != want {
			t.Fatal("完整教育行不能直接作为学历，冲突时不能取第一项")
		}
	}
}
