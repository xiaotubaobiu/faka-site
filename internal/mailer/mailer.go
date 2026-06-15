package mailer

import (
	"fmt"
	"net/smtp"
)

type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

type Mailer struct{ cfg SMTPConfig }

func New(cfg SMTPConfig) *Mailer { return &Mailer{cfg: cfg} }

// Send 发一封纯文本邮件。发信失败返回 error,调用方决定是否阻塞业务。
func (m *Mailer) Send(to, subject, body string) error {
	if m.cfg.Host == "" {
		return fmt.Errorf("smtp not configured")
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Pass, m.cfg.Host)
	msg := []byte(buildMessage(m.cfg.From, to, subject, body))
	return smtp.SendMail(addr, auth, m.cfg.From, []string{to}, msg)
}

func buildMessage(from, to, subject, body string) string {
	return fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)
}
