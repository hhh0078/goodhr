// 本文件复用平台SMTP传输发送招聘邮件，校验邮件头并将回复地址设置为团队联系邮箱。
package httpapi

import (
	"errors"
	"fmt"
	"net/smtp"
	"strings"
)

// recruitmentReplyMailer 为现有Mailer增加可选Reply-To能力，不破坏其他发信调用。
type recruitmentReplyMailer interface {
	SendRecruitmentMail(email, replyTo, subject, htmlBody, plainText string) error
}

// recruitmentMailTransportConfigured 区分真实平台发信与开发日志模式，测试假发信器可继续验证闭环。
func recruitmentMailTransportConfigured(mailer Mailer) bool {
	switch value := mailer.(type) {
	case nil, DevMailer, *DevMailer:
		return false
	case SMTPMailer:
		return value.Host != "" && value.Username != "" && value.Password != ""
	case *SMTPMailer:
		return value != nil && value.Host != "" && value.Username != "" && value.Password != ""
	default:
		return true
	}
}

// sendRecruitmentMail 优先使用平台Reply-To扩展，测试或旧发信器继续复用既有接口。
func sendRecruitmentMail(mailer Mailer, email, replyTo, subject, htmlBody, plainText string) error {
	if err := validateRecruitmentMailHeaders(email, replyTo, subject); err != nil {
		return err
	}
	if extended, ok := mailer.(recruitmentReplyMailer); ok {
		return extended.SendRecruitmentMail(email, replyTo, subject, htmlBody, plainText)
	}
	return mailer.SendCustomHTML(email, subject, htmlBody, plainText)
}

// validateRecruitmentMailHeaders 拒绝地址和标题中的CRLF及无效邮箱，避免邮件头注入。
func validateRecruitmentMailHeaders(email, replyTo, subject string) error {
	for _, value := range []string{email, replyTo, subject} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("邮件标题和地址不能换行")
		}
	}
	if _, valid := normalizeEmail(email); !valid {
		return errors.New("收件邮箱没填完整")
	}
	if _, valid := normalizeEmail(replyTo); !valid {
		return errors.New("回复邮箱没填完整")
	}
	return nil
}

// buildRecruitmentMailMessage 在已校验的标准MIME邮件中增加团队回复地址。
func buildRecruitmentMailMessage(from, email, replyTo, subject, plainText, htmlBody string) (string, error) {
	if err := validateRecruitmentMailHeaders(email, replyTo, subject); err != nil {
		return "", err
	}
	if strings.ContainsAny(from, "\r\n\x00") {
		return "", errors.New("平台发件地址不正确")
	}
	message := buildMailMessage(from, email, subject, plainText, htmlBody)
	message = strings.Replace(message, "\r\nSubject:", "\r\nReply-To: "+replyTo+"\r\nSubject:", 1)
	return message, nil
}

// SendRecruitmentMail 用平台已配置的SMTP账号发送，并将候选人回复交给团队联系人。
func (m SMTPMailer) SendRecruitmentMail(email, replyTo, subject, htmlBody, plainText string) error {
	from := strings.TrimSpace(m.From)
	if from == "" {
		from = m.Username
	}
	message, err := buildRecruitmentMailMessage(from, email, replyTo, subject, plainText, htmlBody)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	auth := smtp.PlainAuth("", m.Username, m.Password, m.Host)
	if m.Port == 465 {
		return m.sendTLS(addr, auth, from, email, message)
	}
	return m.sendSMTP(addr, auth, from, email, message, smtpDeliveryOptions{timeout: smtpSendTimeout})
}
