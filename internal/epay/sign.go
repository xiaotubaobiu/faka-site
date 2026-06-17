package epay

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

func Sign(params url.Values, key string) string {
	keys := make([]string, 0, len(params))
	for k, vs := range params {
		if k == "sign" || k == "sign_type" {
			continue
		}
		if len(vs) == 0 || vs[0] == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params.Get(k))
	}
	raw := strings.Join(parts, "&") + key

	h := md5.Sum([]byte(raw))
	return hex.EncodeToString(h[:])
}

func Verify(params url.Values, key string) bool {
	signature := params.Get("sign")
	if signature == "" {
		return false
	}
	return Sign(params, key) == signature
}
