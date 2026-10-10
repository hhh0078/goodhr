// 本文件定义中文招聘邮件占位符、服务端可信变量和纯文本到安全HTML的渲染规则。
package httpapi

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RecruitmentMailPlaceholder 描述可插入的中文占位符及其来源和示例。
type RecruitmentMailPlaceholder struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	Example  string `json:"example"`
	Required bool   `json:"required"`
	Source   string `json:"source"`
}

// RecruitmentMailPreview 返回渲染内容、缺失变量及示例标记，避免将示例当成真实候选人数据。
type RecruitmentMailPreview struct {
	ContextFingerprint  string   `json:"context_fingerprint,omitempty"`
	Recipient           string   `json:"recipient"`
	ReplyTo             string   `json:"reply_to"`
	Subject             string   `json:"subject"`
	Body                string   `json:"body"`
	HTML                string   `json:"html"`
	MissingPlaceholders []string `json:"missing_placeholders"`
	UnknownPlaceholders []string `json:"unknown_placeholders"`
	CanSend             bool     `json:"can_send"`
	IsExample           bool     `json:"is_example"`
	Warnings            []string `json:"warnings"`
}

var recruitmentMailPlaceholderPattern = regexp.MustCompile(`\{([^{}]+)\}`)

// recruitmentMailPlaceholders 返回分组目录；必须值由当前模板实际使用的变量决定。
func recruitmentMailPlaceholders() []RecruitmentMailPlaceholder {
	return []RecruitmentMailPlaceholder{
		{"简历姓名", "简历姓名", "候选人与岗位", "李同学（示例）", true, "候选人简历"},
		{"岗位名称", "岗位名称", "候选人与岗位", "产品经理（示例）", true, "应聘岗位"},
		{"当前时间", "当前时间", "通用", "2026-10-10 10:00", true, "服务器时间（北京时间）"},
		{"公司名称", "公司名称", "团队公司", "示例科技有限公司", true, "团队公司信息"},
		{"招聘联系人", "招聘联系人", "团队公司", "王招聘（示例）", true, "团队公司信息"},
		{"联系电话", "联系电话", "团队公司", "13800000000（示例）", false, "团队公司信息"},
		{"联系邮箱", "联系邮箱", "团队公司", "hr@example.com", true, "团队公司信息"},
		{"公司地址", "公司地址", "团队公司", "上海市示例路 1 号（示例）", true, "团队公司信息"},
		{"面试节点", "面试节点", "面试安排", "初试（示例）", true, "所选场次对应的流程节点"},
		{"面试时间", "面试时间", "面试安排", "2026-10-20 14:00", true, "所选面试场次（北京时间）"},
		{"面试时长", "面试时长", "面试安排", "45 分钟", true, "所选面试场次"},
		{"面试方式", "面试方式", "面试安排", "视频面试", true, "所选面试场次"},
		{"面试地点", "面试地点", "面试安排", "线上面试，无需到场", true, "所选面试场次"},
		{"会议链接", "会议链接", "面试安排", "https://meeting.example.com/demo", true, "所选面试场次，现场面试显示无需链接"},
		{"确认链接", "确认链接", "面试安排", "https://example.com/interview-invitation/example", true, "服务器生成的受限确认链接"},
		{"入职日期", "入职日期", "录用与入职", "2026-11-01", true, "应聘记录的入职日期"},
	}
}

// recruitmentMailValues 只从团队配置和授权业务上下文生成实际发送变量。
func recruitmentMailValues(company RecruitmentCompany, ctx RecruitmentMailContext, now time.Time) map[string]string {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	values := map[string]string{"简历姓名": ctx.CandidateName, "岗位名称": ctx.PositionName, "当前时间": now.In(zone).Format("2006-01-02 15:04"), "公司名称": company.CompanyName, "招聘联系人": company.ContactName, "联系电话": company.ContactPhone, "联系邮箱": company.ContactEmail, "公司地址": company.CompanyAddress, "面试节点": ctx.NodeName, "面试时间": "", "面试时长": "", "面试方式": ctx.Mode, "面试地点": ctx.Location, "会议链接": ctx.MeetingURL, "确认链接": ctx.ConfirmationURL, "入职日期": ctx.EntryDate, "面试结果": "", "后续安排": "", "录用说明": "", "报到材料": ""}
	if !ctx.InterviewTime.IsZero() {
		values["面试时间"] = ctx.InterviewTime.In(zone).Format("2006-01-02 15:04")
	}
	if ctx.Duration > 0 {
		values["面试时长"] = fmt.Sprintf("%d 分钟", ctx.Duration)
	}
	switch ctx.Mode {
	case "online", "video":
		values["面试方式"] = "视频面试"
		if values["面试地点"] == "" {
			values["面试地点"] = "线上面试，无需到场"
		}
	case "onsite", "offline", "in_person":
		values["面试方式"] = "现场面试"
		values["会议链接"] = "现场面试，无需会议链接"
	case "phone":
		values["面试方式"] = "电话面试"
		values["面试地点"] = "电话沟通，无需到场"
		values["会议链接"] = "电话面试，无需会议链接"
	}
	if values["联系电话"] == "" {
		values["联系电话"] = "请通过联系邮箱与我们沟通"
	}
	return values
}

// recruitmentMailExamples 提供明确标记为示例的数据，不参与真正发送。
func recruitmentMailExamples() map[string]string {
	values := map[string]string{}
	for _, item := range recruitmentMailPlaceholders() {
		values[item.Key] = item.Example
	}
	return values
}

// recruitmentMailCompanySubject 统一加公司名前缀，并去除重复的相同前缀。
func recruitmentMailCompanySubject(companyName, subject string) string {
	prefix := "[" + strings.TrimSpace(companyName) + "]"
	subject = strings.TrimSpace(subject)
	for strings.HasPrefix(subject, prefix) {
		subject = strings.TrimSpace(strings.TrimPrefix(subject, prefix))
	}
	return prefix + " " + subject
}

// renderRecruitmentMail 进行单次占位符替换并转义HTML，缺失或未知变量保留原样供用户检查。
func renderRecruitmentMail(subject, body string, values map[string]string, isExample bool) RecruitmentMailPreview {
	result := RecruitmentMailPreview{MissingPlaceholders: []string{}, UnknownPlaceholders: []string{}, Warnings: []string{}, IsExample: isExample}
	known := map[string]bool{}
	for _, item := range recruitmentMailPlaceholders() {
		known[item.Key] = true
	}
	missing, unknown := map[string]bool{}, map[string]bool{}
	replace := func(input string) string {
		return recruitmentMailPlaceholderPattern.ReplaceAllStringFunc(input, func(match string) string {
			key := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
			if !known[key] {
				unknown[key] = true
				return match
			}
			value := values[key]
			if strings.TrimSpace(value) == "" {
				missing[key] = true
				return match
			}
			return value
		})
	}
	result.Subject = replace(subject)
	result.Body = replace(body)
	for key := range missing {
		result.MissingPlaceholders = append(result.MissingPlaceholders, key)
	}
	for key := range unknown {
		result.UnknownPlaceholders = append(result.UnknownPlaceholders, key)
	}
	sort.Strings(result.MissingPlaceholders)
	sort.Strings(result.UnknownPlaceholders)
	if strings.TrimSpace(values["公司名称"]) == "" {
		if !missing["公司名称"] {
			result.MissingPlaceholders = append(result.MissingPlaceholders, "公司名称")
		}
	} else {
		result.Subject = recruitmentMailCompanySubject(values["公司名称"], result.Subject)
	}
	if strings.ContainsAny(result.Subject, "\r\n") {
		result.Warnings = append(result.Warnings, "邮件标题不能换行，请检查标题和变量")
	}
	if len([]rune(result.Subject)) > 400 {
		result.Warnings = append(result.Warnings, "带公司名称的邮件标题超过400字，请缩短后再发送")
	}
	if len([]rune(result.Body)) > 200000 {
		result.Warnings = append(result.Warnings, "展开变量后的正文超过20万字，请缩短模板或变量内容")
	}
	if strings.ContainsAny(recruitmentMailPlaceholderPattern.ReplaceAllString(subject+body, ""), "{}") {
		result.Warnings = append(result.Warnings, "占位符的花括号没配对，请按目录插入")
	}
	if strings.Contains(subject+body, "【请填写已确认的") {
		result.Warnings = append(result.Warnings, "请先把正文中待填写的结果、录用条件或报到材料改成已确认的具体内容")
	}
	result.HTML = `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"></head><body><div style="font-family:Arial,sans-serif;line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere">` + recruitmentMailLinkedHTML(result.Body, values) + `</div></body></html>`
	if isExample {
		result.Warnings = append(result.Warnings, "当前使用示例数据，真正发送会重新读取候选人和面试安排")
	}
	result.CanSend = !isExample && len(result.MissingPlaceholders) == 0 && len(result.UnknownPlaceholders) == 0 && len(result.Warnings) == 0 && strings.TrimSpace(subject) != "" && strings.TrimSpace(body) != ""
	return result
}

// recruitmentMailLinkedHTML 只将确认与会议变量中的有效HTTP地址转为转义后的链接，其余正文始终是纯文本。
func recruitmentMailLinkedHTML(body string, values map[string]string) string {
	escaped := html.EscapeString(body)
	links := []string{}
	seen := map[string]bool{}
	for _, key := range []string{"确认链接", "会议链接"} {
		value := values[key]
		if value == "" || seen[value] || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
			continue
		}
		seen[value] = true
		links = append(links, value)
	}
	sort.Slice(links, func(i, j int) bool { return len(links[i]) > len(links[j]) })
	pairs := []string{}
	for _, link := range links {
		safe := html.EscapeString(link)
		pairs = append(pairs, safe, `<a href="`+safe+`" rel="noopener noreferrer">`+safe+`</a>`)
	}
	if len(pairs) == 0 {
		return escaped
	}
	return strings.NewReplacer(pairs...).Replace(escaped)
}
