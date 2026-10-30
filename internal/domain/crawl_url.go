package domain

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const MaxCrawlPages = 100

// NormalizeCrawlURL 只规范来源和片段，保留路径编码及有序查询参数。
func NormalizeCrawlURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "\r\n\x00") {
		return "", fmt.Errorf("crawl URL requires HTTPS without credentials, at most 2048 bytes")
	}
	host := strings.ToLower(u.Hostname())
	if strings.IndexFunc(host, func(r rune) bool { return r > 127 }) >= 0 || strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("crawl URL requires an ASCII hostname and valid port")
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	port := u.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", fmt.Errorf("crawl URL port must be between 1 and 65535")
		}
		port = strconv.Itoa(number)
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" && port != "443" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}
