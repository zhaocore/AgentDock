package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
)

// ExportSource 将远程读取与无磁盘转换限制在 Worker 接缝。
type ExportSource interface {
	Tree(context.Context, string) ([]byte, error)
	Page(context.Context, domain.ConfluencePage) ([]ExportFile, error)
}

// ExportFile 是待写入对象存储的内存文件。
type ExportFile struct {
	Name    string `json:"name"`
	Content []byte `json:"content"`
}

// ConfluenceSource 只向部署者指定实例发送 Cookie。
type ConfluenceSource struct {
	Config   config.ConfluenceConfig
	AgentDir string
	HTTP     *http.Client
}

// NewConfluenceSource 拒绝重定向，防止登录页或跨站内容冒充导出成功。
func NewConfluenceSource(cfg config.ConfluenceConfig, agentDir string) *ConfluenceSource {
	return &ConfluenceSource{Config: cfg, AgentDir: agentDir, HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return fmt.Errorf("Confluence redirect rejected") }}}
}

// ValidateConfluenceURL 在发送凭据前限制 HTTPS 实例与上下文路径。
func ValidateConfluenceURL(base, raw string) error {
	b, err := url.Parse(base)
	if err != nil || b.Scheme != "https" || b.Host == "" || b.User != nil || b.RawQuery != "" || b.Fragment != "" {
		return fmt.Errorf("CONFLUENCE_BASE_URL must be a HTTPS instance URL without credentials or query")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, strings.TrimRight(b.Path, "/")+"/") {
		return fmt.Errorf("page URL must belong to configured Confluence instance")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == ".." || part == "." || strings.Contains(part, "\\") {
			return fmt.Errorf("invalid Confluence URL path")
		}
	}
	return nil
}

// script 通过管道交换数据，不让正文落入临时文件。
func script(ctx context.Context, program, file string, input []byte) ([]byte, error) {
	command := exec.CommandContext(ctx, program, file)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", filepath.Base(file), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

// Tree 复用页面树与批量导出的纯规划函数。
func (s *ConfluenceSource) Tree(ctx context.Context, raw string) ([]byte, error) {
	if err := ValidateConfluenceURL(s.Config.BaseURL, raw); err != nil {
		return nil, err
	}
	input, err := json.Marshal(struct {
		Base   string `json:"base_url"`
		Cookie string `json:"cookie"`
		URL    string `json:"url"`
	}{s.Config.BaseURL, s.Config.Cookie, raw})
	if err != nil {
		return nil, err
	}
	return script(ctx, "python3", filepath.Join(s.AgentDir, "skills/confluence-batch-export/scripts/server_tree.py"), input)
}

// get 有界读取且只重试网络、限流和服务器错误。
func (s *ConfluenceSource) get(ctx context.Context, raw string) ([]byte, error) {
	if err := ValidateConfluenceURL(s.Config.BaseURL, raw); err != nil {
		return nil, err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Cookie", s.Config.Cookie)
		response, err := s.HTTP.Do(req)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024*1024+1))
			response.Body.Close()
			if readErr != nil {
				last = fmt.Errorf("Confluence read failed: %w", readErr)
			} else if len(data) > 64*1024*1024 {
				return nil, fmt.Errorf("Confluence response exceeds 64 MiB")
			} else if response.StatusCode == 200 {
				return data, nil
			} else {
				last = fmt.Errorf("Confluence GET path=%s status=%d", req.URL.Path, response.StatusCode)
				if response.StatusCode != 429 && response.StatusCode < 500 {
					return nil, last
				}
			}
		} else {
			last = fmt.Errorf("Confluence GET failed path=%s: %w", req.URL.Path, err)
		}
		if attempt < 2 {
			slog.Warn("retry Confluence request", "attempt", attempt+1, "error", last)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return nil, last
}

// Page 验证实际发布版本，复用现有转换器定位资源及生成 Markdown。
func (s *ConfluenceSource) Page(ctx context.Context, page domain.ConfluencePage) ([]ExportFile, error) {
	if err := ValidateConfluenceURL(s.Config.BaseURL, page.URL); err != nil {
		return nil, err
	}
	data, err := s.get(ctx, s.Config.BaseURL+"/rest/api/content/"+page.ID+"?expand=body.view,version")
	if err != nil {
		return nil, err
	}
	var identity struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Version struct {
			Number int `json:"number"`
		} `json:"version"`
	}
	if err = json.Unmarshal(data, &identity); err != nil {
		return nil, fmt.Errorf("invalid Confluence page JSON: %w", err)
	}
	if identity.ID != page.ID || identity.Title != page.Title || identity.Version.Number != page.Version {
		return nil, fmt.Errorf("Confluence page identity/version changed id=%s expected=%d actual=%d; start a new task to refresh tree", page.ID, page.Version, identity.Version.Number)
	}
	wrapped := append([]byte(`{"success":true,"data":`), data...)
	wrapped = append(wrapped, '}')
	input := struct {
		Page   string        `json:"page"`
		URL    string        `json:"url"`
		Assets *[]ExportFile `json:"assets,omitempty"`
	}{Page: string(wrapped), URL: page.URL}
	request, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	converter := filepath.Join(s.AgentDir, "skills/url-to-markdown/scripts/server.ts")
	planData, err := script(ctx, "bun", converter, request)
	if err != nil {
		return nil, err
	}
	var plan struct {
		Resources []struct {
			URL       string `json:"url"`
			LocalName string `json:"localName"`
		} `json:"resources"`
	}
	if err = json.Unmarshal(planData, &plan); err != nil {
		return nil, fmt.Errorf("decode Confluence resource plan page_id=%s bytes=%d: %w", page.ID, len(planData), err)
	}
	assets := []ExportFile{}
	for _, resource := range plan.Resources {
		content, err := s.get(ctx, resource.URL)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			slog.Warn("Confluence attachment unavailable; preserving original link", "page_id", page.ID, "resource_name", resource.LocalName, "error", err)
			continue
		}
		if bytes.HasPrefix(bytes.ToLower(bytes.TrimSpace(content)), []byte("<!doctype html")) || bytes.HasPrefix(bytes.ToLower(bytes.TrimSpace(content)), []byte("<html")) {
			slog.Warn("Confluence attachment returned HTML; preserving original link", "page_id", page.ID, "resource_name", resource.LocalName)
			continue
		}
		assets = append(assets, ExportFile{Name: resource.LocalName, Content: content})
	}
	input.Assets = &assets
	request, err = json.Marshal(input)
	if err != nil {
		return nil, err
	}
	bundle, err := script(ctx, "bun", converter, request)
	if err != nil {
		return nil, err
	}
	var files []ExportFile
	if err = json.Unmarshal(bundle, &files); err != nil {
		return nil, fmt.Errorf("decode Confluence Markdown bundle page_id=%s bytes=%d: %w", page.ID, len(bundle), err)
	}
	return files, nil
}
