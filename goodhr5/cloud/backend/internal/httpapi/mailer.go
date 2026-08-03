package httpapi

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Mailer interface {
	SendLoginCode(email string, code string) error
	SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error
	SendAIBalanceNotice(email string, notice AIBalanceNotice) error
	SendPositionStatus(email string, notice PositionStatusNotice) error
	SendCustomHTML(email string, subject string, htmlBody string, plainText string) error
}

// SubscriptionRewardNotice 表示会员天数变动提醒邮件内容。
type SubscriptionRewardNotice struct {
	Reason         string
	Days           int
	MemberType     string
	MemberName     string
	ExpiresAt      time.Time
	RemainingDays  int
	AllowAutoReply bool
	Features       []string
	RelatedEmail   string
}

// AIBalanceNotice 表示 AI 余额变动提醒邮件内容。
type AIBalanceNotice struct {
	Reason       string
	ChangeUnits  int64
	BalanceUnits int64
}

// PositionStatusNotice 表示岗位运行完成或失败邮件内容。
type PositionStatusNotice struct {
	PositionName      string
	Status            string
	StatusLabel       string
	TodayGreetedCount int
	RunGreetedCount   int
	RunSkippedCount   int
	ErrorMessage      string
}

// TeamInvitationNotice 表示团队邀请邮件需要展示的内容。
type TeamInvitationNotice struct {
	InviterEmail string
	TeamName     string
	TeamOwner    string
	Role         string
	LoginURL     string
}

type DevMailer struct{}

func (m DevMailer) SendLoginCode(email string, code string) error {
	log.Printf("GoodHR dev login code for %s: %s", email, code)
	return nil
}

// SendSubscriptionReward 在开发模式下记录会员天数变动提醒。
func (m DevMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	log.Printf("GoodHR dev subscription changed for %s: reason=%s member=%s days=%d expires=%s related=%s", email, notice.Reason, notice.MemberName, notice.Days, notice.ExpiresAt.Format(time.RFC3339), notice.RelatedEmail)
	return nil
}

// SendAIBalanceNotice 在开发模式下记录 AI 余额变动提醒。
func (m DevMailer) SendAIBalanceNotice(email string, notice AIBalanceNotice) error {
	log.Printf("GoodHR dev AI balance changed for %s: reason=%s change=%s balance=%s", email, notice.Reason, aiUnitsToYuanString(notice.ChangeUnits), aiUnitsToYuanString(notice.BalanceUnits))
	return nil
}

// SendPositionStatus 在开发模式下记录岗位状态提醒。
func (m DevMailer) SendPositionStatus(email string, notice PositionStatusNotice) error {
	log.Printf("GoodHR dev position status for %s: position=%s status=%s error=%s", email, notice.PositionName, notice.StatusLabel, notice.ErrorMessage)
	return nil
}

// SendCustomHTML 在开发模式下记录自定义邮件发送请求。
func (m DevMailer) SendCustomHTML(email string, subject string, htmlBody string, plainText string) error {
	log.Printf("GoodHR dev custom mail for %s: subject=%s", email, subject)
	return nil
}

// SendTeamInvitation 在开发模式下记录团队邀请邮件。
func (m DevMailer) SendTeamInvitation(email string, notice TeamInvitationNotice) error {
	log.Printf("GoodHR dev team invitation for %s: inviter=%s team=%s", email, notice.InviterEmail, notice.TeamName)
	return nil
}

type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (m SMTPMailer) SendLoginCode(email string, code string) error {
	return m.sendMessage(email, "GoodHR 登录验证码", "login_code.html", map[string]any{
		"Code": code,
	}, []string{
		"你的 GoodHR 登录验证码是：" + code,
		"验证码 5 分钟内有效，请勿转发给他人。",
	})
}

// SendSubscriptionReward 发送会员天数变动提醒邮件。
func (m SMTPMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	reason := strings.TrimSpace(notice.Reason)
	if reason == "" {
		reason = "会员时间调整"
	}
	memberType := strings.TrimSpace(notice.MemberType)
	if memberType == "" {
		memberType = defaultMemberType
	}
	memberName := strings.TrimSpace(notice.MemberName)
	if memberName == "" {
		memberName = memberTypeName(memberType)
	}
	featureText := strings.Join(notice.Features, "、")
	if featureText == "" {
		featureText = "当前套餐的基础招聘能力"
	}
	autoReplyText := "暂不支持"
	if notice.AllowAutoReply {
		autoReplyText = "支持"
	}
	daysText := fmt.Sprintf("%+d 天", notice.Days)
	lines := []string{
		"你好，你的 GoodHR 会员时间有变动。",
		"变动原因：" + reason,
		"变动天数：" + daysText,
		"会员套餐：" + memberName,
		"新的到期时间：" + notice.ExpiresAt.Format("2006-01-02 15:04:05"),
		"剩余时间：" + intString(notice.RemainingDays) + " 天",
		"支持功能：" + featureText,
		"自动回复：" + autoReplyText,
	}
	if strings.TrimSpace(notice.RelatedEmail) != "" {
		lines = append(lines, "关联用户："+strings.TrimSpace(notice.RelatedEmail))
	}
	lines = append(lines, "感谢使用 GoodHR。")
	return m.sendMessage(email, "GoodHR 会员时间变动提醒", "subscription_reward.html", map[string]any{
		"Reason":         reason,
		"DaysText":       daysText,
		"MemberType":     memberType,
		"MemberName":     memberName,
		"ExpiresAt":      notice.ExpiresAt.Format("2006-01-02 15:04:05"),
		"RemainingDays":  notice.RemainingDays,
		"AllowAutoReply": notice.AllowAutoReply,
		"Features":       notice.Features,
		"RelatedEmail":   strings.TrimSpace(notice.RelatedEmail),
	}, lines)
}

// sendAIBalanceNotice 发送 AI 余额变动提醒，未配置邮件服务时安全跳过。
func sendAIBalanceNotice(mailer Mailer, email string, notice AIBalanceNotice) error {
	if mailer == nil || notice.ChangeUnits == 0 {
		return nil
	}
	return mailer.SendAIBalanceNotice(email, notice)
}

// SendAIBalanceNotice 发送 AI 余额变动提醒邮件。
func (m SMTPMailer) SendAIBalanceNotice(email string, notice AIBalanceNotice) error {
	reason := strings.TrimSpace(notice.Reason)
	if reason == "" {
		reason = "AI 余额调整"
	}
	changeText := aiUnitsToYuanString(notice.ChangeUnits)
	if notice.ChangeUnits > 0 {
		changeText = "+" + changeText
	}
	balanceText := aiUnitsToYuanString(notice.BalanceUnits)
	lines := []string{
		"你好，你的 GoodHR AI 余额有变动。",
		"变动原因：" + reason,
		"变动金额：￥" + changeText,
		"当前余额：￥" + balanceText,
		"这是一封自动提醒邮件，不需要回复。",
	}
	return m.sendMessage(email, "GoodHR AI 余额变动提醒", "ai_balance_notice.html", map[string]any{
		"Reason":      reason,
		"ChangeText":  changeText,
		"BalanceText": balanceText,
	}, lines)
}

// SendPositionStatus 发送岗位运行完成或失败提醒邮件。
func (m SMTPMailer) SendPositionStatus(email string, notice PositionStatusNotice) error {
	statusLabel := strings.TrimSpace(notice.StatusLabel)
	if statusLabel == "" {
		statusLabel = "岗位运行结束"
	}
	subject := "GoodHR " + statusLabel + "提醒"
	lines := []string{
		"我小声汇报一下，岗位这轮已经结束。",
		"岗位名称：" + notice.PositionName,
		"今日打招呼：" + intString(notice.TodayGreetedCount),
		"本次打招呼：" + intString(notice.RunGreetedCount),
		"本次跳过：" + intString(notice.RunSkippedCount),
	}
	if strings.TrimSpace(notice.ErrorMessage) != "" {
		lines = append(lines, "失败原因："+strings.TrimSpace(notice.ErrorMessage))
	}
	lines = append(lines, "你可以回到 GoodHR 控制台查看岗位日志。")
	return m.sendMessage(email, subject, "position_status.html", map[string]any{
		"PositionName": notice.PositionName, "Status": notice.Status,
		"StatusLabel": statusLabel, "TodayGreetedCount": notice.TodayGreetedCount,
		"RunGreetedCount": notice.RunGreetedCount, "RunSkippedCount": notice.RunSkippedCount,
		"ErrorMessage": strings.TrimSpace(notice.ErrorMessage),
	}, lines)
}

// SendTeamInvitation 发送团队邀请邮件，用户登录后仍需本人确认才会加入。
func (m SMTPMailer) SendTeamInvitation(email string, notice TeamInvitationNotice) error {
	roleLabel := "普通成员"
	if normalizeTenantRole(notice.Role) == "admin" {
		roleLabel = "团队管理员"
	}
	teamName := strings.TrimSpace(notice.TeamName)
	if teamName == "" {
		teamName = strings.TrimSpace(notice.TeamOwner) + " 的团队"
	}
	return m.sendMessage(email, "GoodHR 团队邀请", "team_invitation.html", map[string]any{
		"InviterEmail": strings.TrimSpace(notice.InviterEmail),
		"TeamName":     teamName,
		"TeamOwner":    strings.TrimSpace(notice.TeamOwner),
		"RoleLabel":    roleLabel,
		"LoginURL":     strings.TrimSpace(notice.LoginURL),
	}, []string{
		"我小声递个邀请。",
		strings.TrimSpace(notice.InviterEmail) + " 邀请你加入 " + teamName + "。",
		"登录 GoodHR 后确认加入，我才会开始搬数据。",
		strings.TrimSpace(notice.LoginURL),
	})
}

// sendTeamInvitationNotice 使用邮件服务发送团队邀请，并兼容测试邮件实现。
func sendTeamInvitationNotice(mailer Mailer, email string, notice TeamInvitationNotice) error {
	if mailer == nil {
		return fmt.Errorf("邮件服务没有初始化")
	}
	if sender, ok := mailer.(interface {
		SendTeamInvitation(string, TeamInvitationNotice) error
	}); ok {
		return sender.SendTeamInvitation(email, notice)
	}
	roleLabel := "普通成员"
	if normalizeTenantRole(notice.Role) == "admin" {
		roleLabel = "团队管理员"
	}
	htmlBody := SMTPMailer{}.renderHTML("team_invitation.html", map[string]any{
		"InviterEmail": strings.TrimSpace(notice.InviterEmail),
		"TeamName":     strings.TrimSpace(notice.TeamName),
		"TeamOwner":    strings.TrimSpace(notice.TeamOwner),
		"RoleLabel":    roleLabel,
		"LoginURL":     strings.TrimSpace(notice.LoginURL),
	})
	return mailer.SendCustomHTML(email, "GoodHR 团队邀请", htmlBody, "登录 GoodHR 后确认是否加入团队："+strings.TrimSpace(notice.LoginURL))
}

// SendCustomHTML 发送超管自定义 HTML 邮件。
func (m SMTPMailer) SendCustomHTML(email string, subject string, htmlBody string, plainText string) error {
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	auth := smtp.PlainAuth("", m.Username, m.Password, m.Host)
	from := strings.TrimSpace(m.From)
	if from == "" {
		from = m.Username
	}
	plainText = strings.TrimSpace(plainText)
	if plainText == "" {
		plainText = htmlToPlainText(htmlBody)
	}
	message := buildMailMessage(from, email, subject, plainText, wrapCustomMailHTML(subject, htmlBody))
	if m.Port == 465 {
		return m.sendTLS(addr, auth, from, email, message)
	}
	return smtp.SendMail(addr, auth, from, []string{email}, []byte(message))
}

// sendMessage 发送一封同时包含纯文本和 HTML 的邮件。
func (m SMTPMailer) sendMessage(email string, subject string, templateName string, data map[string]any, bodyLines []string) error {
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	auth := smtp.PlainAuth("", m.Username, m.Password, m.Host)
	from := m.From
	if from == "" {
		from = m.Username
	}

	message := buildMailMessage(from, email, subject, strings.Join(bodyLines, "\r\n"), m.renderHTML(templateName, data))

	if m.Port == 465 {
		return m.sendTLS(addr, auth, from, email, message)
	}
	return smtp.SendMail(addr, auth, from, []string{email}, []byte(message))
}

// renderHTML 渲染邮件 HTML 模板，失败时返回空字符串并让邮件使用纯文本兜底。
func (m SMTPMailer) renderHTML(templateName string, data map[string]any) string {
	if strings.TrimSpace(templateName) == "" {
		return ""
	}
	body, err := readEmailTemplate(templateName)
	if err != nil {
		log.Printf("读取邮件模板失败 template=%s err=%v", templateName, err)
		return ""
	}
	tpl, err := template.New(templateName).Parse(string(body))
	if err != nil {
		log.Printf("解析邮件模板失败 template=%s err=%v", templateName, err)
		return ""
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, data); err != nil {
		log.Printf("渲染邮件模板失败 template=%s err=%v", templateName, err)
		return ""
	}
	return rendered.String()
}

// readEmailTemplate 从常见运行目录读取邮件模板。
func readEmailTemplate(templateName string) ([]byte, error) {
	var lastErr error
	for _, templatePath := range emailTemplatePaths(templateName) {
		body, err := os.ReadFile(templatePath)
		if err == nil {
			return body, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// emailTemplatePaths 返回邮件模板可能存在的位置。
func emailTemplatePaths(templateName string) []string {
	return []string{
		filepath.Join("templates", "email", templateName),
		filepath.Join("..", "..", "templates", "email", templateName),
	}
}

// buildMailMessage 组装标准 MIME 邮件内容。
func buildMailMessage(from string, to string, subject string, plainBody string, htmlBody string) string {
	boundary := fmt.Sprintf("goodhr-%d", time.Now().UnixNano())
	headers := []string{
		"From: " + formatAddress("GoodHR", from),
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"MIME-Version: 1.0",
		"Date: " + time.Now().Format(time.RFC1123Z),
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"",
	}
	parts := []string{
		buildMailPart(boundary, "text/plain; charset=UTF-8", plainBody),
	}
	if strings.TrimSpace(htmlBody) != "" {
		parts = append(parts, buildMailPart(boundary, "text/html; charset=UTF-8", htmlBody))
	}
	parts = append(parts, "--"+boundary+"--")
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.Join(parts, "\r\n")
}

// buildMailPart 组装 multipart 邮件中的单个正文部分。
func buildMailPart(boundary string, contentType string, body string) string {
	return strings.Join([]string{
		"--" + boundary,
		"Content-Type: " + contentType,
		"Content-Transfer-Encoding: base64",
		"",
		encodeMailBody(body),
	}, "\r\n")
}

// formatAddress 生成带中文显示名的邮件地址头。
func formatAddress(name string, email string) string {
	encodedName := mime.QEncoding.Encode("UTF-8", name)
	return fmt.Sprintf("%s <%s>", encodedName, email)
}

// encodeMailBody 将邮件正文按 UTF-8 base64 编码，提升不同邮箱客户端兼容性。
func encodeMailBody(body string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	chunks := make([]string, 0, len(encoded)/76+1)
	for len(encoded) > 76 {
		chunks = append(chunks, encoded[:76])
		encoded = encoded[76:]
	}
	if encoded != "" {
		chunks = append(chunks, encoded)
	}
	return strings.Join(chunks, "\r\n")
}

// sendTLS 通过 SMTPS 发送邮件。
func (m SMTPMailer) sendTLS(addr string, auth smtp.Auth, from string, to string, message string) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: m.Host,
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// wrapCustomMailHTML 包装移动端友好的自定义邮件 HTML。
func wrapCustomMailHTML(subject string, body string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + template.HTMLEscapeString(subject) + `</title></head><body style="margin:0;background:#f4f7f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#1f2a23;"><div style="max-width:640px;margin:0 auto;padding:20px 14px;"><div style="background:#fff;border:1px solid #e2e8e3;border-radius:12px;padding:22px;line-height:1.8;font-size:15px;"><h1 style="font-size:20px;line-height:1.35;margin:0 0 16px;">` + template.HTMLEscapeString(subject) + `</h1><div style="word-break:break-word;">` + body + `</div></div></div></body></html>`
}

// htmlToPlainText 将 HTML 粗略转成纯文本兜底。
func htmlToPlainText(value string) string {
	text := regexp.MustCompile(`(?is)<br\s*/?>`).ReplaceAllString(value, "\n")
	text = regexp.MustCompile(`(?is)</p>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}
