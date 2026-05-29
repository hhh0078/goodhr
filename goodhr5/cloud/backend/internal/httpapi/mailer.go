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
	"strings"
	"time"
)

type Mailer interface {
	SendLoginCode(email string, code string) error
	SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error
}

// SubscriptionRewardNotice 表示会员天数变动提醒邮件内容。
type SubscriptionRewardNotice struct {
	Reason       string
	Days         int
	MemberType   string
	ExpiresAt    time.Time
	RelatedEmail string
}

type DevMailer struct{}

func (m DevMailer) SendLoginCode(email string, code string) error {
	log.Printf("GoodHR dev login code for %s: %s", email, code)
	return nil
}

// SendSubscriptionReward 在开发模式下记录会员天数变动提醒。
func (m DevMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	log.Printf("GoodHR dev subscription changed for %s: reason=%s days=%d expires=%s related=%s", email, notice.Reason, notice.Days, notice.ExpiresAt.Format(time.RFC3339), notice.RelatedEmail)
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
	daysText := fmt.Sprintf("%+d 天", notice.Days)
	lines := []string{
		"你好，你的 GoodHR 会员时间有变动。",
		"变动原因：" + reason,
		"变动天数：" + daysText,
		"会员类型：" + memberType,
		"新的到期时间：" + notice.ExpiresAt.Format("2006-01-02 15:04:05"),
	}
	if strings.TrimSpace(notice.RelatedEmail) != "" {
		lines = append(lines, "关联用户："+strings.TrimSpace(notice.RelatedEmail))
	}
	lines = append(lines, "感谢使用 GoodHR。")
	return m.sendMessage(email, "GoodHR 会员时间变动提醒", "subscription_reward.html", map[string]any{
		"Reason":       reason,
		"DaysText":     daysText,
		"MemberType":   memberType,
		"ExpiresAt":    notice.ExpiresAt.Format("2006-01-02 15:04:05"),
		"RelatedEmail": strings.TrimSpace(notice.RelatedEmail),
	}, lines)
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
	templatePath := filepath.Join("templates", "email", templateName)
	body, err := os.ReadFile(templatePath)
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
