package web

import "testing"

func TestValidateNewPassword(t *testing.T) {
	cases := []struct{ pw, confirm, want string }{
		{"", "", "密码至少 6 位"},
		{"123", "123", "密码至少 6 位"},
		{"123456", "654321", "两次密码不一致"},
		{"123456", "123456", ""},
		{"abcdef", "abcdef", ""},
	}
	for _, c := range cases {
		if got := validateNewPassword(c.pw, c.confirm); got != c.want {
			t.Fatalf("validate(%q,%q)=%q want %q", c.pw, c.confirm, got, c.want)
		}
	}
}
