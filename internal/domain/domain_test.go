// Package domain 领域模型 JSON 契约测试。
package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// JSON 字段名是 API 契约：字段改名即破坏下游消费方，此处锁定关键契约。
func TestRepositoryJSONContract(t *testing.T) {
	repo := Repository{
		ID: "repo-1", Name: "web", GitURL: "https://example.com/web.git",
		RepoType: RepoTypeFrontend, Branch: "main", Status: RepoStatusReady,
	}

	raw, err := json.Marshal(repo)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, key := range []string{
		`"id"`, `"name"`, `"git_url"`, `"repo_type"`, `"branch"`,
		`"local_path"`, `"status"`, `"last_error"`, `"created_at"`, `"updated_at"`,
	} {
		if !strings.Contains(body, key) {
			t.Errorf("Repository JSON should contain %s, got %s", key, body)
		}
	}
}

// Agent JSON 契约：skills / mcp_servers / subagents 是配置面的核心字段。
func TestAgentJSONContract(t *testing.T) {
	agent := Agent{
		ID: "agent-1", OwnerUserID: "user-1", Name: "bot",
		SystemPrompt: "你是助手", Model: "gpt-4o-mini",
		Skills:     []string{"search"},
		MCPServers: []MCPServerConfig{{Name: "fs", Transport: "stdio", Command: "mcp-fs"}},
		Subagents:  []SubagentDef{{Name: "researcher", Description: "查资料"}},
	}

	raw, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, key := range []string{
		`"id"`, `"owner_user_id"`, `"system_prompt"`, `"model"`,
		`"skills"`, `"mcp_servers"`, `"subagents"`,
	} {
		if !strings.Contains(body, key) {
			t.Errorf("Agent JSON should contain %s, got %s", key, body)
		}
	}
}

// 状态枚举值是状态机的对外命名，改动即破坏 API 消费方与前端展示。
func TestStatusEnumValues(t *testing.T) {
	repoCases := map[RepoStatus]string{
		RepoStatusPending: "pending", RepoStatusCloning: "cloning",
		RepoStatusReady: "ready", RepoStatusFailed: "failed",
	}
	for status, want := range repoCases {
		if string(status) != want {
			t.Errorf("repo status %q should serialize as %q", status, want)
		}
	}
	agentCases := map[AgentStatus]string{
		AgentStatusCreated: "created", AgentStatusRunning: "running",
		AgentStatusStopped: "stopped", AgentStatusFailed: "failed",
	}
	for status, want := range agentCases {
		if string(status) != want {
			t.Errorf("agent status %q should serialize as %q", status, want)
		}
	}
}

// Message.AgentMeta 是中间步骤元数据透传通道，必须能承载任意 JSON。
func TestMessageAgentMetaPassthrough(t *testing.T) {
	msg := Message{
		ID: "m-1", SessionID: "s-1", Role: "assistant", Content: "hi",
		AgentMeta: json.RawMessage(`{"total_tokens":42}`),
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"total_tokens":42`) {
		t.Errorf("agent_meta must pass through, got %s", raw)
	}
}
