package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zhaocore/agentdock/internal/domain"
)

// fakeRuntime 测试用运行时：记录构建与执行调用。
type fakeRuntime struct {
	buildErr   error
	executeOut string
	executeErr error
	built      []string
}

func (f *fakeRuntime) Build(_ context.Context, agent domain.Agent) (Executor, error) {
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	f.built = append(f.built, agent.ID)
	return fakeExecutor{out: f.executeOut, err: f.executeErr}, nil
}

func (f *fakeRuntime) Execute(ctx context.Context, agent domain.Agent, history []domain.Message, userMessage string) (string, json.RawMessage, error) {
	exec, err := f.Build(ctx, agent)
	if err != nil {
		return "", nil, err
	}
	return exec.Execute(ctx, history, userMessage)
}

type fakeExecutor struct {
	out string
	err error
}

func (f fakeExecutor) Execute(_ context.Context, _ []domain.Message, _ string) (string, json.RawMessage, error) {
	if f.err != nil {
		return "", nil, f.err
	}
	return f.out, json.RawMessage(`{}`), nil
}

// composeSystemPrompt 必须把 skills / MCP / subagent 注入提示词，
// 否则 Agent 定义形同虚设。
func TestComposeSystemPromptIncludesAllSections(t *testing.T) {
	agent := domain.Agent{
		SystemPrompt: "你是助手",
		Skills:       []string{"search", "write"},
		MCPServers:   []domain.MCPServerConfig{{Name: "fs"}},
		Subagents:    []domain.SubagentDef{{Name: "researcher", Description: "查资料"}},
	}
	got := composeSystemPrompt(agent)
	for _, want := range []string{"你是助手", "search, write", "fs", "researcher", "查资料"} {
		if !contains(got, want) {
			t.Errorf("system prompt missing %q:\n%s", want, got)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
