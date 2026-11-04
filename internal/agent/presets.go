package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/zhaocore/agentdock/internal/domain"
)

var presetName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}$`)

// LoadPresets 校验整套部署配置并生成公共定义，不修改磁盘或数据库。
func LoadPresets(root, model string) ([]domain.Agent, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("LLM_MODEL must not be empty")
	}
	mcpPath := filepath.Join(root, "mcp.json")
	mcpJSON, err := readPresetFile(mcpPath)
	if err != nil {
		return nil, err
	}
	var servers []domain.MCPServerConfig
	if err := json.Unmarshal([]byte(mcpJSON), &servers); err != nil {
		return nil, fmt.Errorf("decode MCP configuration %s: %w", mcpPath, err)
	}
	if servers == nil {
		return nil, fmt.Errorf("MCP configuration %s must be an array", mcpPath)
	}
	names := make(map[string]bool)
	for _, server := range servers {
		if !presetName.MatchString(server.Name) || names[server.Name] {
			return nil, fmt.Errorf("MCP configuration %s has invalid or duplicate server name %q", mcpPath, server.Name)
		}
		names[server.Name] = true
		if (server.Transport != "stdio" && server.Transport != "sse") ||
			(server.Transport == "stdio" && strings.TrimSpace(server.Command) == "") ||
			(server.Transport == "sse" && strings.TrimSpace(server.Endpoint) == "") {
			return nil, fmt.Errorf("MCP server %q in %s requires stdio with command or sse with endpoint", server.Name, mcpPath)
		}
	}

	skillDir := filepath.Join(root, "skills")
	skills, err := os.ReadDir(skillDir)
	if err != nil {
		return nil, fmt.Errorf("read skills directory %s: %w", skillDir, err)
	}
	skillNames := []string{}
	var skillPrompt strings.Builder
	for _, skill := range skills {
		if strings.HasPrefix(skill.Name(), ".") {
			continue
		}
		if !skill.IsDir() || !presetName.MatchString(skill.Name()) {
			return nil, fmt.Errorf("skill %q in %s must be a directory with a valid name", skill.Name(), skillDir)
		}
		body, err := readPresetFile(filepath.Join(skillDir, skill.Name(), "SKILL.md"))
		if err != nil {
			return nil, err
		}
		skillNames = append(skillNames, skill.Name())
		skillPrompt.WriteString("\n\n## Skill: " + skill.Name() + "\n" + body)
	}

	presetDir := filepath.Join(root, "presets")
	entries, err := os.ReadDir(presetDir)
	if err != nil {
		return nil, fmt.Errorf("read presets directory %s: %w", presetDir, err)
	}
	agents := []domain.Agent{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if !strings.HasSuffix(entry.Name(), ".md") || !presetName.MatchString(name) {
			return nil, fmt.Errorf("preset %q in %s must have a valid name and .md extension", entry.Name(), presetDir)
		}
		prompt, err := readPresetFile(filepath.Join(presetDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		agents = append(agents, domain.Agent{
			ID:   uuid.NewSHA1(uuid.NameSpaceURL, []byte("AgentDock/presets/"+name)).String(),
			Name: name, SystemPrompt: prompt + skillPrompt.String(), Model: model,
			Skills: skillNames, MCPServers: servers, Subagents: []domain.SubagentDef{},
		})
	}
	return agents, nil
}

// readPresetFile 拒绝空文件及符号链接，避免加载目录外文件。
func readPresetFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect agent configuration %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("agent configuration %s must be a regular file", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read agent configuration %s: %w", path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("agent configuration %s must not be empty", path)
	}
	return string(body), nil
}
