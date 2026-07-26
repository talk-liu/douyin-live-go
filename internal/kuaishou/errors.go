package kuaishou

import "strings"

// IsRateLimitErr 判断是否为快手接口限流/风控拒绝（与浏览器能否打开页面无直接关系）。
func IsRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "限流") ||
		strings.Contains(msg, "请求过快") ||
		strings.Contains(msg, "result=2") ||
		strings.Contains(msg, "稍后重试")
}
