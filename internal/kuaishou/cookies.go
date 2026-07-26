package kuaishou

import "strings"

func isValidCookieValue(value string) bool {
	return !strings.ContainsAny(value, "\"\n\r;")
}
