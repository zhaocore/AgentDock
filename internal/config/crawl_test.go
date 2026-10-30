package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCrawlProfiles(t *testing.T) {
	user := "11111111-1111-1111-1111-111111111111"
	valid := CrawlProfile{ID: "docs", Origin: "https://docs.example", Cookie: "session=secret; second=value", UserIDs: []string{user}, ReadySelector: "[data-ready]"}
	raw, _ := json.Marshal([]CrawlProfile{valid})
	profiles, err := ParseCrawlProfiles(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CrawlAccess(profiles, user, "docs", []string{"https://docs.example/page"}); err != nil {
		t.Fatal(err)
	}
	for _, urls := range [][]string{nil, {"https://else.example/page"}, {"https://docs.example/#bad"}, {"https://name:secret@docs.example/page"}, {"https://docs.example/a", "https://docs.example/a"}, strings.Split(strings.Repeat("https://docs.example/page,", 22), ",")} {
		if _, err = CrawlAccess(profiles, user, "docs", urls); err == nil {
			t.Fatal("invalid URLs accepted", urls)
		}
	}
	for _, u := range []string{"other", ""} {
		if _, err = CrawlAccess(profiles, u, "docs", []string{valid.Origin + "/"}); err == nil {
			t.Fatal("unauthorized profile")
		}
	}
	for _, input := range []string{"{", "null", `[{}]`, strings.Replace(string(raw), "https://docs.example", "http://docs.example", 1), strings.Replace(string(raw), "https://docs.example", "https://docs.example/path", 1), strings.Replace(string(raw), user, "bad", 1), strings.Replace(string(raw), "session=secret; second=value", "bad", 1), "[" + string(raw[1:len(raw)-1]) + "," + string(raw[1:len(raw)-1]) + "]"} {
		parsed, err := ParseCrawlProfiles(input)
		if input == "null" {
			if err == nil && len(parsed) == 0 {
				continue
			}
		}
		if err == nil {
			t.Fatal("invalid profile accepted", input)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("credential leak")
		}
	}
	if p, err := ParseCrawlProfiles(""); err != nil || len(p) != 0 {
		t.Fatal(p, err)
	}
}

// TestCrawlCanonicalOrigins 确保浏览器不会在任务保存后才拒绝来源写法。
func TestCrawlCanonicalOrigins(t *testing.T) {
	for _, origin := range []string{"https://docs.example", "https://docs.example:8443", "https://[::1]", "https://[2001:db8::1]:8443"} {
		raw, _ := json.Marshal([]CrawlProfile{{ID: "docs", Origin: origin, Cookie: "session=test", UserIDs: []string{"11111111-1111-1111-1111-111111111111"}, ReadySelector: "main"}})
		profiles, err := ParseCrawlProfiles(string(raw))
		if err != nil {
			t.Fatal(origin, err)
		}
		if _, err = CrawlAccess(profiles, profiles[0].UserIDs[0], "docs", []string{origin + "/page"}); err != nil {
			t.Fatal(err)
		}
		if _, err = CrawlAccess(profiles, profiles[0].UserIDs[0], "docs", []string{"https://else.example/page"}); err == nil {
			t.Fatal("different origin accepted")
		}
	}
	for _, origin := range []string{"https://DOCS.example", "https://docs.example:443", "https://docs.example:0443", "https://docs.example:08443", "https://docs.example:65536", "https://docs.example:", "https://[0:0:0:0:0:0:0:1]", "https://例子.example", "https://[fe80::1%25en0]"} {
		raw, _ := json.Marshal([]CrawlProfile{{ID: "docs", Origin: origin, Cookie: "session=test", UserIDs: []string{"11111111-1111-1111-1111-111111111111"}, ReadySelector: "main"}})
		if _, err := ParseCrawlProfiles(string(raw)); err == nil {
			t.Fatal("noncanonical origin accepted", origin)
		}
	}
}
