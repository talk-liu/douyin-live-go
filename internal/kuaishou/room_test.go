package kuaishou

import (
	"testing"
)

func TestExtractPrincipalID(t *testing.T) {
	cases := map[string]string{
		"https://live.kuaishou.com/u/ACHY828-": "ACHY828-",
		"ACHY828-":                             "ACHY828-",
		"https://live.kuaishou.com/u/abc?x=1":  "abc",
	}
	for input, want := range cases {
		if got := ExtractPrincipalID(input); got != want {
			t.Fatalf("ExtractPrincipalID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsRateLimitError(t *testing.T) {
	if !isRateLimitError("请求过快，请稍后重试") {
		t.Fatal("expected rate limit")
	}
	if isRateLimitError("直播间不存在") {
		t.Fatal("unexpected rate limit")
	}
}

func TestSanitizeStateJSON(t *testing.T) {
	raw := `{"a":undefined,"b":[undefined]}`
	got := sanitizeStateJSON(raw)
	if got == raw {
		t.Fatal("expected sanitized json")
	}
}
