package payment

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ConfigFingerprint returns a stable digest of the inputs that determine a
// provider's behaviour: the relevant config KV map values plus the *contents*
// of the key files (so re-uploading a key file is detected, but touching its
// mtime is not). The result is used by Registry.SetProvidersIfChanged to skip
// redundant rebuilds, which is what eliminates the per-request race.
//
// Only the keys that actually influence provider construction are inspected,
// so unrelated config edits (e.g. SMTP) never trigger a rebuild.
func ConfigFingerprint(cfg map[string]string, keyFiles map[string]string) string {
	relevant := []string{
		"alipay_appid", "alipay_gateway", "alipay_sandbox",
		"wxpay_appid", "wxpay_mchid", "wxpay_serial_no",
	}
	var b strings.Builder
	for _, k := range relevant {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(cfg[k])
		b.WriteByte('\n')
	}
	// Key files: iterate a fixed, sorted list for determinism.
	for _, name := range sortedKeyFileNames(keyFiles) {
		b.WriteString("file:")
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(keyFiles[name])
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func sortedKeyFileNames(m map[string]string) []string {
	// Fixed canonical order matches KeyFileSpecs so two equal inputs always
	// produce identical fingerprints regardless of map iteration order.
	specs := []string{"alipay_private.pem", "alipay_public.pem", "wxpay_private.pem", "wxpay_apiv3.key"}
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		if _, ok := m[s]; ok {
			out = append(out, s)
		}
	}
	return out
}
