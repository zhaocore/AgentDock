package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdatedPresetReachesLLMOnNextTurn(t *testing.T) {
	type requestMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatRequest struct {
		Model    string           `json:"model"`
		Messages []requestMessage `json:"messages"`
	}
	requests := make(chan chatRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	root := presetTestRoot(t)
	rt := NewLangChainRuntime("test-key", server.URL)
	for _, prompt := range []string{"first role prompt", "updated role prompt"} {
		writePresetTestFile(t, root, "presets/agent1.md", prompt)
		presets, err := LoadPresets(root, "shared-model")
		if err != nil {
			t.Fatal(err)
		}
		answer, _, err := rt.Execute(context.Background(), presets[0], nil, "hello")
		if err != nil || answer != "ok" {
			t.Fatalf("execute: %q %v", answer, err)
		}
		request := <-requests
		if request.Model != "shared-model" || len(request.Messages) < 2 || request.Messages[0].Role != "system" || !strings.Contains(request.Messages[0].Content, prompt) || !strings.Contains(request.Messages[0].Content, "检查权限边界") {
			t.Fatalf("LLM missed synchronized configuration: %+v", request)
		}
	}
}
