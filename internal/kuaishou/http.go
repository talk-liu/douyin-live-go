package kuaishou

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

func newHTTPClient(cookie string) *http.Client {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
	}
	if cookie != "" {
		injectCookies(jar, cookie)
	}
	return client
}

func injectCookies(jar *cookiejar.Jar, cookie string) {
	for _, domain := range []string{"https://live.kuaishou.com/", "https://kuaishou.com/"} {
		u, err := url.Parse(domain)
		if err != nil {
			continue
		}
		var cookies []*http.Cookie
		for _, part := range strings.Split(cookie, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, value, ok := strings.Cut(part, "=")
			if !ok || name == "" {
				continue
			}
			cookies = append(cookies, &http.Cookie{Name: name, Value: value})
		}
		if len(cookies) > 0 {
			jar.SetCookies(u, cookies)
		}
	}
}

func cloneHeaders(base http.Header, referer string) http.Header {
	h := base.Clone()
	if referer != "" {
		h.Set("Referer", referer)
	}
	return h
}
