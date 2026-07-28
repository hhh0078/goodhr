// Package common 文件作用：验证公共岗位名称清理和搜索词规则不会随平台改动退化。
package common

import "testing"

// TestPositionSearchQuery 验证岗位配置后缀和中英文括号备注会被清理。
func TestPositionSearchQuery(t *testing.T) {
	cases := map[string]string{
		"Java开发 _ 上海":    "Java开发",
		"Java开发（初级）":     "Java开发",
		"Java开发(可接受应届生)": "Java开发",
		" Java 开发 ":      "Java 开发",
	}
	for input, expected := range cases {
		if actual := PositionSearchQuery(input); actual != expected {
			t.Fatalf("岗位搜索词不正确：input=%q actual=%q expected=%q", input, actual, expected)
		}
	}
}

// TestPageURLMatches 验证入口地址允许保留用户筛选参数，但会拒绝登录页跳转。
func TestPageURLMatches(t *testing.T) {
	if !PageURLMatches(
		"https://www.zhipin.com/web/chat/recommend?city=101280100",
		"https://www.zhipin.com/web/chat/recommend",
	) {
		t.Fatal("带筛选参数的入口页应当匹配")
	}
	if PageURLMatches(
		"https://login.zhipin.com/?redirect=%2Fweb%2Fchat%2Frecommend",
		"https://www.zhipin.com/web/chat/recommend",
	) {
		t.Fatal("登录页跳转不应被当作入口页")
	}
	if PageURLMatches(
		"https://login.zhipin.com/?redirect=https://www.zhipin.com/web/chat/recommend",
		"https://www.zhipin.com/web/chat/recommend",
	) {
		t.Fatal("查询参数包含目标地址时也不应误判")
	}
}

// TestCandidateFingerprint 验证普通平台候选人仍只按姓名和年龄生成稳定编号。
func TestCandidateFingerprint(t *testing.T) {
	first := CandidateFingerprint("boss", "范召", nil, "范召 29岁 本科 5年 带货主播")
	second := CandidateFingerprint("boss", "范召", nil, "范召 29岁 大专 8年 直播运营")
	if first != "boss_范召_29" || second != first {
		t.Fatalf("同名同年龄候选人编号不稳定：first=%q second=%q", first, second)
	}
	if value := CandidateFingerprint("boss", "范召", nil, "范召 本科 5年"); value != "" {
		t.Fatalf("缺少年龄时不应生成稳定编号：%q", value)
	}
}
