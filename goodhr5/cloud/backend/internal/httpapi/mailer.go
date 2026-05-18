package httpapi

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/smtp"
	"strings"
)

type Mailer interface {
	SendLoginCode(email string, code string) error
}

type DevMailer struct{}

func (m DevMailer) SendLoginCode(email string, code string) error {
	log.Printf("GoodHR dev login code for %s: %s", email, code)
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
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	auth := smtp.PlainAuth("", m.Username, m.Password, m.Host)
	from := m.From
	if from == "" {
		from = m.Username
	}

	message := strings.Join([]string{
		"From: " + from,
		"To: " + email,
		"Subject: GoodHR 登录验证码",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"你的 GoodHR 登录验证码是：" + code,
		"验证码 5 分钟内有效，请勿转发给他人。",
	}, "\r\n")

	if m.Port == 465 {
		return m.sendTLS(addr, auth, from, email, message)
	}
	return smtp.SendMail(addr, auth, from, []string{email}, []byte(message))
}

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
