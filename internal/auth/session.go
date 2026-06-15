package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
)

type Session struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	CSRF   string `json:"csrf"`
	Exp    int64  `json:"exp"`
}

var ErrInvalidSession = errors.New("invalid session")

const sessionTTL = 7 * 24 * 3600

func NewSession(userID int64, role string, now int64) (Session, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return Session{}, err
	}
	return Session{UserID: userID, Role: role, CSRF: hex.EncodeToString(buf), Exp: now + sessionTTL}, nil
}

// EncodeSession: base64url(json) + "." + hex(hmac-sha256)
func EncodeSession(s Session, secret []byte) (string, error) {
	body, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	return enc + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func DecodeSession(cookie string, secret []byte, now int64) (*Session, error) {
	parts := split2(cookie, '.')
	if len(parts) != 2 {
		return nil, ErrInvalidSession
	}
	enc, macHex := parts[0], parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	if !hmac.Equal([]byte(macHex), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return nil, ErrInvalidSession
	}
	body, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, ErrInvalidSession
	}
	var s Session
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, ErrInvalidSession
	}
	if now > s.Exp {
		return nil, ErrInvalidSession
	}
	return &s, nil
}

func NewCSRFToken() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

func split2(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
