package payment

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// KeyDir is the directory where payment PEM/key files live. It is configured
// once at startup (from an env var or default "keys") and then read on demand
// whenever a provider is built.
var (
	keyDirOnce sync.Once
	keyDir     = "keys"
)

// SetKeyDir sets the directory used for payment key files. Called once at
// startup. Falls back to "keys" (relative to the working directory).
func SetKeyDir(dir string) {
	keyDirOnce.Do(func() {
		if dir = strings.TrimSpace(dir); dir != "" {
			keyDir = dir
		}
	})
	// ensure dir exists; ignore error (uploads will create it lazily)
	_ = os.MkdirAll(keyDir, 0o700)
}

// KeyDir returns the active key directory.
func KeyDir() string {
	SetKeyDir("")
	return keyDir
}

// ReadKeyFile reads a key file from the key directory. Returns "" (no error)
// when the file is absent, so callers treat "not yet uploaded" as simply
// "channel unconfigured" rather than a hard failure.
func ReadKeyFile(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(KeyDir(), name))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// WriteKeyFile writes a key file (mode 0600) into the key directory. Used by the
// upload endpoint.
func WriteKeyFile(name, content string) error {
	if err := os.MkdirAll(KeyDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(KeyDir(), name), []byte(content), 0o600)
}

// DeleteKeyFile removes a key file. Missing file is a no-op (so the UI "clear"
// action is idempotent).
func DeleteKeyFile(name string) error {
	err := os.Remove(filepath.Join(KeyDir(), name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// KeyFileExists reports whether a key file is present and non-empty.
func KeyFileExists(name string) bool {
	s, err := ReadKeyFile(name)
	return err == nil && s != ""
}

// KeyFileStatus lists the known key files and whether each is present. Used by
// the config UI to render upload buttons vs "已上传" badges.
type KeyFileStatus struct {
	Name        string // filename, e.g. "alipay_private.pem"
	Label       string // human label, e.g. "应用私钥"
	Channel     string // "alipay" | "wxpay"
	Description string // what to put here
	Present     bool
}

// KeyFileSpecs is the fixed list of key files the system looks for.
var KeyFileSpecs = []KeyFileStatus{
	{Name: "alipay_private.pem", Label: "应用私钥", Channel: "alipay", Description: "支付宝开放平台「应用私钥」PKCS#8 PEM"},
	{Name: "alipay_public.pem", Label: "支付宝公钥", Channel: "alipay", Description: "用于验签回调的支付宝公钥 PEM"},
	{Name: "wxpay_private.pem", Label: "商户私钥", Channel: "wxpay", Description: "微信支付商户 API 证书私钥 RSA2048 PEM"},
	{Name: "wxpay_apiv3.key", Label: "APIv3 密钥", Channel: "wxpay", Description: "微信支付 APIv3 密钥(32位字符串)"},
}

// KeyFileStatuses returns the current presence of every known key file.
func KeyFileStatuses() []KeyFileStatus {
	out := make([]KeyFileStatus, len(KeyFileSpecs))
	for i, s := range KeyFileSpecs {
		s.Present = KeyFileExists(s.Name)
		out[i] = s
	}
	return out
}
