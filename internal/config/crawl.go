package config

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// CrawlProfile 由部署者授权给指定用户，仅对精确 HTTPS 来源注入 Cookie。
type CrawlProfile struct {
	ID            string   `json:"id"`
	Origin        string   `json:"origin"`
	Cookie        string   `json:"cookie"`
	UserIDs       []string `json:"user_ids"`
	ReadySelector string   `json:"ready_selector"`
}

// ParseCrawlProfiles 校验部署配置，不将凭据值拼入错误。
func ParseCrawlProfiles(raw string) ([]CrawlProfile, error) {
	if raw == "" {
		return []CrawlProfile{}, nil
	}
	var profiles []CrawlProfile
	if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
		return nil, fmt.Errorf("CRAWL_PROFILES must be a JSON array")
	}
	if profiles == nil || len(profiles) > 100 {
		return nil, fmt.Errorf("CRAWL_PROFILES must be an array with at most 100 profiles")
	}
	seen := map[string]bool{}
	for i, p := range profiles {
		u, err := url.Parse(p.Origin)
		if p.ID == "" || len(p.ID) > 80 || seen[p.ID] || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || p.ReadySelector == "" || len(p.UserIDs) == 0 || p.Cookie == "" || strings.ContainsAny(p.Cookie, "\r\n\x00") {
			return nil, fmt.Errorf("CRAWL_PROFILES[%d] requires unique id, exact HTTPS origin, cookie, ready_selector and user_ids", i)
		}
		if u.Host != strings.ToLower(u.Host) || u.Port() == "443" || strings.IndexFunc(u.Host, func(r rune) bool { return r > 127 }) >= 0 || strings.Contains(u.Host, "%") {
			return nil, fmt.Errorf("CRAWL_PROFILES[%d] origin must use a lowercase ASCII host and omit the default 443 port", i)
		}
		if ip, err := netip.ParseAddr(u.Hostname()); err == nil && ip.Is6() && ip.String() != u.Hostname() {
			return nil, fmt.Errorf("CRAWL_PROFILES[%d] origin must use compressed IPv6 notation", i)
		}
		if port := u.Port(); port != "" {
			value, err := strconv.Atoi(port)
			if err != nil || value > 65535 || strconv.Itoa(value) != port {
				return nil, fmt.Errorf("CRAWL_PROFILES[%d] origin requires a canonical port between 0 and 65535", i)
			}
		}
		if strings.HasSuffix(u.Host, ":") {
			return nil, fmt.Errorf("CRAWL_PROFILES[%d] origin cannot contain an empty port", i)
		}
		for _, pair := range strings.Split(p.Cookie, ";") {
			if !strings.Contains(strings.TrimSpace(pair), "=") || strings.HasPrefix(strings.TrimSpace(pair), "=") {
				return nil, fmt.Errorf("CRAWL_PROFILES[%d] cookie must contain name=value pairs", i)
			}
		}
		for _, id := range p.UserIDs {
			if _, err := uuid.Parse(id); err != nil {
				return nil, fmt.Errorf("CRAWL_PROFILES[%d] contains invalid user_id", i)
			}
		}
		seen[p.ID] = true
	}
	return profiles, nil
}

// CrawlAccess 验证每个页面和凭据引用都属于用户获授权的来源。
func CrawlAccess(profiles []CrawlProfile, userID, profileID string, urls []string) (CrawlProfile, error) {
	for _, p := range profiles {
		if p.ID != profileID || !slices.Contains(p.UserIDs, userID) {
			continue
		}
		if len(urls) < 1 || len(urls) > 20 {
			return CrawlProfile{}, fmt.Errorf("crawl requires 1–20 URLs")
		}
		seen := map[string]bool{}
		for _, raw := range urls {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme+"://"+u.Host != p.Origin || u.User != nil || u.Fragment != "" || len(raw) > 2048 || seen[raw] {
				return CrawlProfile{}, fmt.Errorf("crawl URLs must be unique HTTPS pages belonging to selected origin, without credentials or fragments")
			}
			seen[raw] = true
		}
		return p, nil
	}
	return CrawlProfile{}, fmt.Errorf("crawl login profile is unavailable or not authorized for this user")
}
