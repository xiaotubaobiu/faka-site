package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"faka-site/internal/auth"
	"faka-site/internal/store"
)

type fakeMailer struct{ got string }

func (f *fakeMailer) Send(to, subject, body string) error { f.got = to; return nil }

// newForgotServer 构造一个用内存库 + 假 mailer 的 Server(不读配置、不碰真实 DB)。
func newForgotServer(t *testing.T, seedEmail string) (*Server, *fakeMailer) {
	t.Helper()
	st, _ := store.OpenInMemory()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if seedEmail != "" {
		_, _ = st.CreateUser(seedEmail, "seedhash", "user") // hash 会被 postForgot 覆盖,占位即可
	}
	fm := &fakeMailer{}
	return &Server{store: st, throttle: auth.NewThrottle(100), now: time.Now, mailSender: fm}, fm
}

func TestPostForgot_SendsForExistingUser(t *testing.T) {
	s, fm := newForgotServer(t, "real@x.com")
	req := httptest.NewRequest("POST", "/forgot", strings.NewReader("email=real@x.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.postForgot(rec, req)
	if fm.got != "real@x.com" {
		t.Fatalf("expected mail to real@x.com, got %q", fm.got)
	}
	if !strings.Contains(rec.Body.String(), "新密码已发送") {
		t.Fatalf("expected uniform sent message, got: %s", rec.Body.String())
	}
}

func TestPostForgot_NoMailForUnknownUser(t *testing.T) {
	s, fm := newForgotServer(t, "real@x.com")
	req := httptest.NewRequest("POST", "/forgot", strings.NewReader("email=ghost@x.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.postForgot(rec, req)
	if fm.got != "" {
		t.Fatalf("must not mail unknown user, but mailed %q", fm.got)
	}
	if !strings.Contains(rec.Body.String(), "新密码已发送") {
		t.Fatalf("must show same uniform message to avoid enumeration, got: %s", rec.Body.String())
	}
}
