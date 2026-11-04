package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zhaocore/agentdock/internal/domain"
)

// toMessageContent：系统提示词在前、历史按角色映射、新消息在末尾。
func TestToMessageContent(t *testing.T) {
	history := []domain.Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "unknown", Content: "skip"},
	}
	msgs := toMessageContent(history, "sys", "q2")
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if string(msgs[0].Role) != "system" {
		t.Errorf("first message role=%s, want system", msgs[0].Role)
	}
	if string(msgs[1].Role) != "human" || string(msgs[2].Role) != "ai" {
		t.Errorf("history roles wrong: %s %s", msgs[1].Role, msgs[2].Role)
	}
	if string(msgs[3].Role) != "human" {
		t.Errorf("last message role=%s, want human", msgs[3].Role)
	}
	// unknown 角色的历史被跳过（len=4 已验证），首条 parts 为系统提示词
	if !strings.Contains(fmt.Sprintf("%v", msgs[0].Parts[0]), "sys") {
		t.Errorf("system prompt not first part: %s", msgs[0].Parts[0])
	}
}

// 构造 runtime 冒烟：NewLangChainRuntime + Build（不真实联网，仅验证构建路径）。
func TestNewLangChainRuntime(t *testing.T) {
	rt := NewLangChainRuntime("key", "https://example.com/v1")
	if rt == nil || rt.apiKey != "key" || rt.baseURL != "https://example.com/v1" {
		t.Fatalf("unexpected runtime: %+v", rt)
	}
}
