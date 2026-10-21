package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

// Executor 是一次构建产物：可复用的 agent 执行器。
type Executor interface {
	// Execute 以历史消息 + 新用户输入执行一轮 agent 调用。
	Execute(ctx context.Context, history []domain.Message, userMessage string) (string, json.RawMessage, error)
}

// Runtime 构建 agent 执行器并执行对话；接口化以便测试注入。
type Runtime interface {
	// Build 按 Agent 定义构建执行器（系统提示词、skills、MCP、subagent）。
	Build(ctx context.Context, agent domain.Agent) (Executor, error)
	// Execute 直接执行一轮对话（管理器内部使用）。
	Execute(ctx context.Context, agent domain.Agent, history []domain.Message, userMessage string) (string, json.RawMessage, error)
}

// LangChainRuntime 基于 langchaingo 的 OpenAI 兼容运行时。
type LangChainRuntime struct {
	apiKey  string
	baseURL string
}

// NewLangChainRuntime 创建 langchaingo 运行时。
func NewLangChainRuntime(apiKey, baseURL string) *LangChainRuntime {
	return &LangChainRuntime{apiKey: apiKey, baseURL: baseURL}
}

// Build 构建 executor：按 Agent 定义创建 OpenAI 兼容模型客户端，
// skills / MCP / subagent 以系统提示词段落注入（MCP stdio/sse 工具
// 桥接与 subagent 工具化调用待后续版本接入 langchaingo agent 循环）。
func (r *LangChainRuntime) Build(_ context.Context, agent domain.Agent) (Executor, error) {
	opts := []openai.Option{
		openai.WithToken(r.apiKey),
		openai.WithModel(agent.Model),
	}
	if r.baseURL != "" {
		opts = append(opts, openai.WithBaseURL(r.baseURL))
	}
	model, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create openai model: %w", err)
	}
	return &langchainExecutor{model: model, agent: agent}, nil
}

// Execute 构建后执行。
func (r *LangChainRuntime) Execute(ctx context.Context, agent domain.Agent, history []domain.Message, userMessage string) (string, json.RawMessage, error) {
	executor, err := r.Build(ctx, agent)
	if err != nil {
		return "", nil, err
	}
	return executor.Execute(ctx, history, userMessage)
}

// langchainExecutor 单实例执行器。
type langchainExecutor struct {
	model llms.Model
	agent domain.Agent
}

// composeSystemPrompt 组装系统提示词：基础提示 + skills + MCP + subagent 声明。
func composeSystemPrompt(agent domain.Agent) string {
	var b strings.Builder
	b.WriteString(agent.SystemPrompt)
	if len(agent.Skills) > 0 {
		b.WriteString("\n\n可用 skills：" + strings.Join(agent.Skills, ", "))
	}
	if len(agent.MCPServers) > 0 {
		names := make([]string, 0, len(agent.MCPServers))
		for _, s := range agent.MCPServers {
			names = append(names, s.Name)
		}
		b.WriteString("\n\n可用 MCP 服务：" + strings.Join(names, ", "))
	}
	if len(agent.Subagents) > 0 {
		b.WriteString("\n\n可委派的 subagent：")
		for _, sub := range agent.Subagents {
			b.WriteString("\n- " + sub.Name + ": " + sub.Description)
		}
	}
	return b.String()
}

// toMessageContent 把领域消息转换为 langchaingo 消息。
func toMessageContent(history []domain.Message, systemPrompt, userMessage string) []llms.MessageContent {
	msgs := []llms.MessageContent{{
		Role:  llms.ChatMessageTypeSystem,
		Parts: []llms.ContentPart{llms.TextContent{Text: systemPrompt}},
	}}
	for _, m := range history {
		switch m.Role {
		case "user":
			msgs = append(msgs, llms.MessageContent{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextContent{Text: m.Content}},
			})
		case "assistant":
			msgs = append(msgs, llms.MessageContent{
				Role:  llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{llms.TextContent{Text: m.Content}},
			})
		}
	}
	msgs = append(msgs, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: userMessage}},
	})
	return msgs
}

// Execute 执行一轮对话，回复与用量元数据一并返回。
func (e *langchainExecutor) Execute(ctx context.Context, history []domain.Message, userMessage string) (string, json.RawMessage, error) {
	msgs := toMessageContent(history, composeSystemPrompt(e.agent), userMessage)
	resp, err := e.model.GenerateContent(ctx, msgs)
	if err != nil {
		return "", nil, fmt.Errorf("agent generate: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", nil, fmt.Errorf("agent generate returned no choices")
	}
	choice := resp.Choices[0]
	meta := map[string]any{"generations": len(resp.Choices)}
	if choice.GenerationInfo != nil {
		// openaiclient 在 GenerationInfo 内携带 token 用量等原始信息
		meta["generation_info"] = choice.GenerationInfo
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", nil, fmt.Errorf("encode agent meta: %w", err)
	}
	return choice.Content, raw, nil
}
