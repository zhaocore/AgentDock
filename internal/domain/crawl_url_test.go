package domain

import "testing"

func TestNormalizeCrawlURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://DOCS.example:443/a?x=1&x=2#section":   "https://docs.example/a?x=1&x=2",
		"https://docs.example":                         "https://docs.example/",
		"https://[0:0:0:0:0:0:0:1]:8443/a%2Fb?b=2&a=1": "https://[::1]:8443/a%2Fb?b=2&a=1",
		"https://docs.example:08443/":                  "https://docs.example:8443/",
	} {
		got, err := NormalizeCrawlURL(raw)
		if err != nil || got != want {
			t.Fatalf("%q => %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"http://docs.example", "https://u:p@docs.example/", "https://docs.example:0/", "https://docs.example:65536/", "https://docs.example:/", "https://例子.example/", "https:///a", "%", "https://docs.example/\n"} {
		if _, err := NormalizeCrawlURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
