package httpapi

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net/smtp"
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
	return m.sendMessage(email, "GoodHR 登录验证码", []string{
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
	return m.sendMessage(email, "GoodHR 会员时间变动提醒", lines)
}

// sendMessage 发送一封纯文本邮件。
func (m SMTPMailer) sendMessage(email string, subject string, bodyLines []string) error {
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	auth := smtp.PlainAuth("", m.Username, m.Password, m.Host)
	from := m.From
	if from == "" {
		from = m.Username
	}
	fromHeader := formatAddress("GoodHR", from)

	message := strings.Join([]string{
		"From: " + fromHeader,
		"To: " + email,
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"MIME-Version: 1.0",
		"Date: " + time.Now().Format(time.RFC1123Z),
		"Content-Transfer-Encoding: base64",
		"Content-Type: text/plain; charset=UTF-8",
	}, "\r\n") + "\r\n\r\n" + encodeMailBody(bodyLines)

	if m.Port == 465 {
		return m.sendTLS(addr, auth, from, email, message)
	}
	return smtp.SendMail(addr, auth, from, []string{email}, []byte(message))
}

// formatAddress 生成带中文显示名的邮件地址头。
func formatAddress(name string, email string) string {
	encodedName := mime.QEncoding.Encode("UTF-8", name)
	return fmt.Sprintf("%s <%s>", encodedName, email)
}

// encodeMailBody 将邮件正文按 UTF-8 base64 编码，提升不同邮箱客户端兼容性。
func encodeMailBody(bodyLines []string) string {
	body := strings.Join(bodyLines, "\r\n")
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
