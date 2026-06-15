package web

import (
	"fmt"
	"strconv"
)

// quotaPerUnit: 1 美元 = 500000 NewAPI quota(与 NewAPI 的 QuotaPerUnit 一致)。
const quotaPerUnit = 500000

// usd 把 quota 格式化成美元字符串展示。
func usd(quota int64) string {
	return fmt.Sprintf("$%.2f", float64(quota)/float64(quotaPerUnit))
}

// usdToQuota 把用户输入的美元字符串换算成 quota。
func usdToQuota(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(f * quotaPerUnit), nil
}
