// 本文件验证邮件模板、MIME格式及本地SMTP传输的截止时间和确认边界，不外发真实邮件。
package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMailTemplatesRender 验证现有邮件模板可以正常渲染关键内容。
func TestMailTemplatesRender(t *testing.T) {
	mailer := SMTPMailer{}
	loginHTML := mailer.renderHTML("login_code.html", map[string]any{"Code": "1234"})
	if !strings.Contains(loginHTML, "1234") || !strings.Contains(loginHTML, "验证码来啦") {
		t.Fatalf("login template did not render expected content: %s", loginHTML)
	}

	rewardHTML := mailer.renderHTML("subscription_reward.html", map[string]any{
		"Reason":         "新用户注册赠送会员",
		"DaysText":       "+3 天",
		"MemberType":     "max",
		"MemberName":     "Max 全能版",
		"ExpiresAt":      time.Date(2026, 6, 2, 12, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
		"RemainingDays":  3,
		"AllowAutoReply": true,
		"Features":       []string{"AI筛选", "AI自动回复"},
		"RelatedEmail":   "",
	})
	if !strings.Contains(rewardHTML, "新用户注册赠送会员") ||
		!strings.Contains(rewardHTML, "Max 全能版") ||
		!strings.Contains(rewardHTML, "AI自动回复") {
		t.Fatalf("reward template did not render expected content: %s", rewardHTML)
	}

	balanceHTML := mailer.renderHTML("ai_balance_notice.html", map[string]any{
		"Reason":      "活动赠送",
		"ChangeText":  "+10.00",
		"BalanceText": "20.00",
	})
	if !strings.Contains(balanceHTML, "活动赠送") || !strings.Contains(balanceHTML, "20.00") {
		t.Fatalf("balance template did not render expected content: %s", balanceHTML)
	}

	positionHTML := mailer.renderHTML("position_status.html", map[string]any{
		"PositionName":      "带货主播",
		"Status":            "failed",
		"StatusLabel":       "岗位运行失败",
		"TodayGreetedCount": 12,
		"RunGreetedCount":   3,
		"RunSkippedCount":   6,
		"ErrorMessage":      "本地程序断开",
	})
	if !strings.Contains(positionHTML, "今日打招呼") || !strings.Contains(positionHTML, "本次跳过") || !strings.Contains(positionHTML, "本地程序断开") {
		t.Fatalf("position template did not render expected content: %s", positionHTML)
	}

	invitationHTML := mailer.renderHTML("team_invitation.html", map[string]any{
		"InviterEmail": "owner@example.com", "TeamName": "招聘一组", "TeamOwner": "owner@example.com",
		"RoleLabel": "普通成员", "LoginURL": "https://goodhr5.58it.cn/admin",
	})
	if !strings.Contains(invitationHTML, "<!doctype html>") || !strings.Contains(invitationHTML, "招聘一组") || !strings.Contains(invitationHTML, "登录后查看邀请") {
		t.Fatalf("team invitation template did not render expected content: %s", invitationHTML)
	}
}

// TestBuildMailMessageHasSingleSubject 验证邮件只包含一个主题头。
func TestBuildMailMessageHasSingleSubject(t *testing.T) {
	message := buildMailMessage("from@example.com", "to@example.com", "GoodHR 测试", "hello", "<p>hello</p>")
	if count := strings.Count(message, "\r\nSubject: "); count != 1 {
		t.Fatalf("subject header count = %d, want 1", count)
	}
}

// smtpTestServerOptions 指定本地SMTP夹具在哪个阶段等待、拒绝或断开连接。
type smtpTestServerOptions struct {
	implicitTLS   bool
	startTLS      bool
	hangAt        string
	dropAfterDATA bool
	dropOnQUIT    bool
	rcptReply     int
	dataReply     int
	responseDelay time.Duration
}

// smtpTestServerResult 记录本地服务真正收到的正文和命令，避免只验证错误文字。
type smtpTestServerResult struct {
	message  []byte
	commands []string
	err      error
}

// smtpTestCertificate 生成仅供本地夹具使用的证书及信任池，不读取任何真实密钥。
func smtpTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local-smtp-fixture"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	serverCertificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return serverCertificate, pool
}

// smtpTestServer 启动只监听回环地址的一次性服务，清理时关闭连接和监听端口。
func smtpTestServer(t *testing.T, options smtpTestServerOptions) (SMTPMailer, smtpDeliveryOptions, <-chan smtpTestServerResult) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var serverCertificate tls.Certificate
	var pool *x509.CertPool
	if options.implicitTLS || options.startTLS {
		serverCertificate, pool = smtpTestCertificate(t)
	}
	accepted := make(chan net.Conn, 1)
	finished := make(chan smtpTestServerResult, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			finished <- smtpTestServerResult{err: acceptErr}
			return
		}
		accepted <- conn
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		finished <- smtpTestServe(conn, options, serverCertificate)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	mailer := SMTPMailer{Host: host, Port: portNumber, Username: "fixture-user", Password: "fixture-only-password", From: "fixture@example.com"}
	delivery := smtpDeliveryOptions{timeout: time.Second, implicitTLS: options.implicitTLS}
	if pool != nil {
		delivery.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: pool}
	}
	return mailer, delivery, finished
}

// smtpTestServe 实际处理SMTP、STARTTLS及DATA正文，指定等待阶段直到客户端关闭连接。
func smtpTestServe(conn net.Conn, options smtpTestServerOptions, certificate tls.Certificate) smtpTestServerResult {
	var result smtpTestServerResult
	if options.hangAt == "tls" {
		_, _ = io.Copy(io.Discard, conn)
		return result
	}
	encrypted := options.implicitTLS
	if encrypted {
		secure := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		if err := secure.Handshake(); err != nil {
			result.err = err
			return result
		}
		conn = secure
	}
	if options.hangAt == "banner" {
		_, _ = io.Copy(io.Discard, conn)
		return result
	}
	protocol := textproto.NewConn(conn)
	if err := protocol.PrintfLine("220 local SMTP fixture"); err != nil {
		result.err = err
		return result
	}
	for {
		line, err := protocol.ReadLine()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				result.err = err
			}
			return result
		}
		command := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		result.commands = append(result.commands, command)
		if options.hangAt == strings.ToLower(command) {
			_, _ = io.Copy(io.Discard, conn)
			return result
		}
		if options.responseDelay > 0 {
			time.Sleep(options.responseDelay)
		}
		switch command {
		case "EHLO":
			if options.startTLS && !encrypted {
				err = protocol.PrintfLine("250-local SMTP fixture\r\n250-STARTTLS\r\n250 AUTH PLAIN")
			} else {
				err = protocol.PrintfLine("250-local SMTP fixture\r\n250 AUTH PLAIN")
			}
		case "STARTTLS":
			err = protocol.PrintfLine("220 ready for TLS")
			if err == nil {
				if options.hangAt == "starttls-handshake" {
					_, _ = io.Copy(io.Discard, conn)
					return result
				}
				secure := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
				err = secure.Handshake()
				if err == nil {
					conn, encrypted = secure, true
					protocol = textproto.NewConn(conn)
				}
			}
		case "AUTH":
			err = protocol.PrintfLine("235 authentication succeeded")
		case "MAIL":
			err = protocol.PrintfLine("250 sender accepted")
		case "RCPT":
			code := options.rcptReply
			if code == 0 {
				code = 250
			}
			err = protocol.PrintfLine("%d recipient result", code)
		case "DATA":
			err = protocol.PrintfLine("354 send body")
			if err == nil && options.hangAt == "body-write" {
				// 不读取正文，形成真实套接字背压，夹具稍后退出以便检查发送协程已结束。
				time.Sleep(700 * time.Millisecond)
				return result
			}
			if err == nil {
				result.message, err = protocol.ReadDotBytes()
			}
			if err == nil {
				if options.dropAfterDATA {
					return result
				}
				if options.hangAt == "data-confirmation" {
					_, _ = io.Copy(io.Discard, conn)
					return result
				}
				code := options.dataReply
				if code == 0 {
					code = 250
				}
				err = protocol.PrintfLine("%d body result", code)
			}
		case "QUIT":
			if options.dropOnQUIT {
				return result
			}
			result.err = protocol.PrintfLine("221 goodbye")
			return result
		default:
			err = protocol.PrintfLine("500 unsupported fixture command")
		}
		if err != nil {
			result.err = err
			return result
		}
	}
}

// smtpTestResult 等待本地服务退出并返回实际观察，避免测试完成后遗留发送协程。
func smtpTestResult(t *testing.T, finished <-chan smtpTestServerResult) smtpTestServerResult {
	t.Helper()
	select {
	case result := <-finished:
		return result
	case <-time.After(4 * time.Second):
		t.Fatal("本地SMTP服务没有在截止时间内退出")
		return smtpTestServerResult{}
	}
}

// TestSMTPSendDeadline 验证建连后各阶段等待均被真实连接截止中断，最终确认与QUIT有不同结果。
func TestSMTPSendDeadline(t *testing.T) {
	for _, test := range []struct {
		name      string
		options   smtpTestServerOptions
		uncertain bool
		accepted  bool
	}{
		{name: "SMTP欢迎语", options: smtpTestServerOptions{hangAt: "banner"}},
		{name: "隐式TLS握手", options: smtpTestServerOptions{implicitTLS: true, hangAt: "tls"}},
		{name: "STARTTLS握手", options: smtpTestServerOptions{startTLS: true, hangAt: "starttls-handshake"}},
		{name: "认证等待", options: smtpTestServerOptions{hangAt: "auth"}},
		{name: "收件人等待", options: smtpTestServerOptions{hangAt: "rcpt"}},
		{name: "DATA开始等待", options: smtpTestServerOptions{hangAt: "data"}},
		{name: "DATA确认等待", options: smtpTestServerOptions{hangAt: "data-confirmation"}, uncertain: true},
		{name: "QUIT等待不推翻接收", options: smtpTestServerOptions{hangAt: "quit"}, accepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mailer, options, finished := smtpTestServer(t, test.options)
			options.timeout = 100 * time.Millisecond
			started := time.Now()
			err := mailer.sendSMTP(fmt.Sprintf("%s:%d", mailer.Host, mailer.Port), smtp.PlainAuth("", mailer.Username, mailer.Password, mailer.Host), mailer.From, "recipient@example.com", "Subject: fixture\r\n\r\nbody\r\n", options)
			elapsed := time.Since(started)
			if elapsed > time.Second || (err == nil) != test.accepted || IsMailDeliveryUncertain(err) != test.uncertain {
				t.Fatalf("截止或确认分类不符：elapsed=%s, err=%v", elapsed, err)
			}
			if !test.accepted {
				var network net.Error
				if !errors.As(err, &network) || !network.Timeout() || IsMailDeliveryTemporary(err) == test.uncertain {
					t.Fatalf("超时没有保留真实网络结果或安全重试分类：%v", err)
				}
			}
			observed := smtpTestResult(t, finished)
			if (len(observed.message) > 0) != (test.uncertain || test.accepted) {
				t.Fatalf("服务端正文观察不符：bytes=%d, commands=%v", len(observed.message), observed.commands)
			}
		})
	}
}

// TestSMTPSendTotalDeadline 验证连续慢响应共享总截止时间，而非每条SMTP命令重新计时。
func TestSMTPSendTotalDeadline(t *testing.T) {
	mailer, options, finished := smtpTestServer(t, smtpTestServerOptions{responseDelay: 45 * time.Millisecond})
	options.timeout = 110 * time.Millisecond
	started := time.Now()
	err := mailer.sendSMTP(fmt.Sprintf("%s:%d", mailer.Host, mailer.Port), smtp.PlainAuth("", mailer.Username, mailer.Password, mailer.Host), mailer.From, "recipient@example.com", "Subject: fixture\r\n\r\nbody", options)
	if err == nil || IsMailDeliveryUncertain(err) || !IsMailDeliveryTemporary(err) || time.Since(started) > time.Second {
		t.Fatalf("慢响应绕过总截止时间或分类错误：elapsed=%s, err=%v", time.Since(started), err)
	}
	if result := smtpTestResult(t, finished); len(result.message) != 0 {
		t.Fatal("总截止时间到期后仍发送了正文")
	}
}

// TestSMTPBodyWriteDeadline 验证收件端不读取大正文时真实写入会到期，并且不会补发结束符接收半封邮件。
func TestSMTPBodyWriteDeadline(t *testing.T) {
	mailer, options, finished := smtpTestServer(t, smtpTestServerOptions{hangAt: "body-write"})
	options.timeout = 350 * time.Millisecond
	message := "Subject: local backpressure fixture\r\n\r\n" + strings.Repeat("a", 16<<20)
	started := time.Now()
	err := mailer.sendSMTP(fmt.Sprintf("%s:%d", mailer.Host, mailer.Port), smtp.PlainAuth("", mailer.Username, mailer.Password, mailer.Host), mailer.From, "recipient@example.com", message, options)
	var operation *net.OpError
	if !errors.As(err, &operation) || operation.Op != "write" || !operation.Timeout() || IsMailDeliveryUncertain(err) || !IsMailDeliveryTemporary(err) || time.Since(started) > time.Second {
		t.Fatalf("正文写入未按时停止或分类不符：elapsed=%s, err=%v", time.Since(started), err)
	}
	observed := smtpTestResult(t, finished)
	if len(observed.message) != 0 || !strings.Contains(strings.Join(observed.commands, ","), "DATA") {
		t.Fatalf("正文堵塞夹具没有进入预期阶段：commands=%v", observed.commands)
	}
}

// TestSMTPRejectsUntrustedTLS 验证真实TLS证书校验保留，错误证书不被视为临时可重试发送。
func TestSMTPRejectsUntrustedTLS(t *testing.T) {
	mailer, options, finished := smtpTestServer(t, smtpTestServerOptions{implicitTLS: true})
	options.tlsConfig = nil
	err := mailer.sendSMTP(fmt.Sprintf("%s:%d", mailer.Host, mailer.Port), nil, mailer.From, "recipient@example.com", "body", options)
	var verification *tls.CertificateVerificationError
	if !errors.As(err, &verification) || IsMailDeliveryUncertain(err) || IsMailDeliveryTemporary(err) {
		t.Fatalf("未信任证书没有明确停止发送：%v", err)
	}
	if observed := smtpTestResult(t, finished); len(observed.commands) != 0 || len(observed.message) != 0 {
		t.Fatal("证书校验失败后仍发送SMTP身份或正文")
	}
}

// TestSMTPConfirmationBoundary 验证DATA拒绝、确认丢失及确认成功后的QUIT失败不会混为同一种可重试错误。
func TestSMTPConfirmationBoundary(t *testing.T) {
	for _, test := range []struct {
		name      string
		options   smtpTestServerOptions
		accepted  bool
		uncertain bool
		temporary bool
	}{
		{name: "临时收件人拒绝", options: smtpTestServerOptions{rcptReply: 451}, temporary: true},
		{name: "永久收件人拒绝", options: smtpTestServerOptions{rcptReply: 550}},
		{name: "DATA临时拒绝", options: smtpTestServerOptions{dataReply: 451}, temporary: true},
		{name: "DATA永久拒绝", options: smtpTestServerOptions{dataReply: 554}},
		{name: "DATA确认断线", options: smtpTestServerOptions{dropAfterDATA: true}, uncertain: true},
		{name: "DATA非标准确认", options: smtpTestServerOptions{dataReply: 221}, uncertain: true},
		{name: "确认后QUIT断线", options: smtpTestServerOptions{dropOnQUIT: true}, accepted: true},
		{name: "隐式TLS确认后QUIT断线", options: smtpTestServerOptions{implicitTLS: true, dropOnQUIT: true}, accepted: true},
		{name: "STARTTLS确认后QUIT断线", options: smtpTestServerOptions{startTLS: true, dropOnQUIT: true}, accepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mailer, options, finished := smtpTestServer(t, test.options)
			err := mailer.sendSMTP(fmt.Sprintf("%s:%d", mailer.Host, mailer.Port), smtp.PlainAuth("", mailer.Username, mailer.Password, mailer.Host), mailer.From, "recipient@example.com", "Subject: fixture\r\n\r\nbody\r\n", options)
			if (err == nil) != test.accepted || IsMailDeliveryUncertain(err) != test.uncertain || IsMailDeliveryTemporary(err) != test.temporary {
				t.Fatalf("SMTP确认分类不符：err=%v, uncertain=%t, temporary=%t", err, IsMailDeliveryUncertain(err), IsMailDeliveryTemporary(err))
			}
			observed := smtpTestResult(t, finished)
			if test.options.rcptReply == 0 && len(observed.message) == 0 {
				t.Fatalf("本地服务未收到实际正文：commands=%v, err=%v", observed.commands, observed.err)
			}
			if test.options.rcptReply != 0 && len(observed.message) != 0 {
				t.Fatal("收件人被拒绝后仍发送正文")
			}
		})
	}
}

// TestSMTPSubscriptionRewardUsesTransport 验证公开会员通知方法沿用模板并正确处理已接收后的QUIT失败。
func TestSMTPSubscriptionRewardUsesTransport(t *testing.T) {
	mailer, _, finished := smtpTestServer(t, smtpTestServerOptions{dropOnQUIT: true})
	err := mailer.SendSubscriptionReward("recipient@example.com", SubscriptionRewardNotice{Reason: "公众号首次关注奖励", Days: 2, MemberType: "max", MemberName: "Max 全能版", ExpiresAt: time.Now().Add(2 * 24 * time.Hour), RemainingDays: 2})
	if err != nil {
		t.Fatalf("会员邮件已接收后仍被判失败：%v", err)
	}
	observed := smtpTestResult(t, finished)
	if !strings.Contains(string(observed.message), "multipart/alternative") || !strings.Contains(string(observed.message), "Subject:") || len(observed.message) < 200 {
		t.Fatal("会员通知没有实际发送原MIME模板正文")
	}
}

// TestSMTPCustomHTMLUsesTransport 验证自定义邮件也使用同一确认边界，不再经过无截止的标准SendMail。
func TestSMTPCustomHTMLUsesTransport(t *testing.T) {
	mailer, _, finished := smtpTestServer(t, smtpTestServerOptions{dropOnQUIT: true})
	if err := mailer.SendCustomHTML("recipient@example.com", "本地测试", "<p>仅发送到本地夹具</p>", "仅发送到本地夹具"); err != nil {
		t.Fatalf("自定义邮件已接收后仍被判失败：%v", err)
	}
	if observed := smtpTestResult(t, finished); !strings.Contains(string(observed.message), "multipart/alternative") || len(observed.message) < 200 {
		t.Fatal("自定义邮件没有实际发送MIME正文")
	}
}

// TestSMTPDialFailureClassification 验证真实回环端口建连失败和过短建连预算均可安全重试。
func TestSMTPDialFailureClassification(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	mailer := SMTPMailer{Host: "127.0.0.1"}
	for _, timeout := range []time.Duration{time.Second, time.Nanosecond} {
		started := time.Now()
		err := mailer.sendSMTP(address, nil, "fixture@example.com", "recipient@example.com", "body", smtpDeliveryOptions{timeout: timeout})
		if err == nil || IsMailDeliveryUncertain(err) || !IsMailDeliveryTemporary(err) || time.Since(started) > time.Second {
			t.Fatalf("建连失败分类或截止不符：timeout=%s, err=%v", timeout, err)
		}
	}
}

// TestSMTPClassificationHelpers 验证包裹后的不确定错误优先级、SMTP返回码和未知配置错误的保守分类。
func TestSMTPClassificationHelpers(t *testing.T) {
	for _, test := range []struct {
		err       error
		uncertain bool
		temporary bool
	}{
		{err: nil},
		{err: errors.New("邮件模板配置无效")},
		{err: fmt.Errorf("SMTP步骤失败：%w", &textproto.Error{Code: 421, Msg: "busy"}), temporary: true},
		{err: &textproto.Error{Code: 554, Msg: "MI:STC"}},
		{err: fmt.Errorf("外层发送失败：%w", &mailDeliveryUncertainError{cause: io.EOF}), uncertain: true},
		{err: &mailDeliveryUncertainError{cause: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection lost")}}, uncertain: true},
	} {
		if IsMailDeliveryUncertain(test.err) != test.uncertain || IsMailDeliveryTemporary(test.err) != test.temporary {
			t.Fatalf("通知worker分类不符：err=%v", test.err)
		}
	}
	if smtpSendTimeout != 30*time.Second {
		t.Fatalf("生产总发送截止时间不符：%s", smtpSendTimeout)
	}
}
