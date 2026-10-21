package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePresetTestFile(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func presetTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writePresetTestFile(t, root, "mcp.json", `[{"name":"tools","transport":"stdio","command":"tools-server","env":{"TOKEN":"${TOOLS_TOKEN}"}}]`)
	writePresetTestFile(t, root, "skills/review/SKILL.md", "检查权限边界")
	writePresetTestFile(t, root, "skills/.gitkeep", "")
	writePresetTestFile(t, root, "presets/agent1.md", "你是 agent1")
	writePresetTestFile(t, root, "presets/agent2.md", "你是 agent2")
	return root
}

func TestLoadPresetsSharedConfigAndPromptSync(t *testing.T) {
	root := presetTestRoot(t)
	before, err := LoadPresets(root, "shared-model")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 || before[0].OwnerUserID != "" || before[0].Model != before[1].Model ||
		before[0].Skills[0] != "review" || before[1].MCPServers[0].Command != "tools-server" ||
		!strings.Contains(before[0].SystemPrompt, "检查权限边界") {
		t.Fatalf("unexpected presets: %+v", before)
	}
	writePresetTestFile(t, root, "presets/agent1.md", "新的角色提示词")
	writePresetTestFile(t, root, "skills/review/SKILL.md", "新的共享技能")
	after, err := LoadPresets(root, "new-model")
	if err != nil {
		t.Fatal(err)
	}
	if before[0].ID != after[0].ID || !strings.Contains(after[0].SystemPrompt, "新的角色提示词") ||
		!strings.Contains(after[1].SystemPrompt, "新的共享技能") || after[0].Model != "new-model" {
		t.Fatalf("sync changed identity or missed updates: %+v", after)
	}
	if err := os.Remove(filepath.Join(root, "presets/agent1.md")); err != nil {
		t.Fatal(err)
	}
	remaining, err := LoadPresets(root, "shared-model")
	if err != nil || len(remaining) != 1 || remaining[0].ID != before[1].ID {
		t.Fatalf("remove preset: %+v %v", remaining, err)
	}
}

func TestLoadPresetsRejectsInvalidConfiguration(t *testing.T) {
	cases := []struct{ path, body, want string }{
		{"mcp.json", "{", "decode MCP"},
		{"mcp.json", "null", "must be an array"},
		{"mcp.json", "", "must not be empty"},
		{"mcp.json", `[{"name":"x","transport":"stdio"}]`, "requires stdio"},
		{"mcp.json", `[{"name":"x","transport":"sse"}]`, "requires stdio"},
		{"mcp.json", `[{"name":"x","transport":"other"}]`, "requires stdio"},
		{"mcp.json", `[{"name":"bad name","transport":"stdio","command":"x"}]`, "invalid or duplicate"},
		{"mcp.json", `[{"name":"x","transport":"stdio","command":"x"},{"name":"x","transport":"stdio","command":"x"}]`, "invalid or duplicate"},
		{"presets/agent1.md", "   ", "must not be empty"},
		{"presets/bad name.md", "prompt", "valid name"},
		{"presets/config.json", "{}", ".md extension"},
		{"skills/loose.md", "skill", "must be a directory"},
		{"skills/bad name/SKILL.md", "skill", "valid name"},
	}
	for _, tc := range cases {
		t.Run(tc.path+tc.want+tc.body, func(t *testing.T) {
			root := presetTestRoot(t)
			writePresetTestFile(t, root, tc.path, tc.body)
			got, err := LoadPresets(root, "model")
			if err == nil || !strings.Contains(err.Error(), tc.want) || got != nil {
				t.Fatalf("want atomic failure %q, got %+v %v", tc.want, got, err)
			}
		})
	}
}

func TestLoadPresetsMissingFilesAndDirectories(t *testing.T) {
	for _, path := range []string{"mcp.json", "skills", "presets", "skills/review/SKILL.md"} {
		t.Run(path, func(t *testing.T) {
			root := presetTestRoot(t)
			if err := os.RemoveAll(filepath.Join(root, path)); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadPresets(root, "model"); err == nil {
				t.Fatal("missing configuration must fail")
			}
		})
	}
	if _, err := LoadPresets(presetTestRoot(t), " "); err == nil {
		t.Fatal("empty model must fail")
	}
}

func TestLoadPresetsRejectsSymlinkAndAllowsEmptyCatalog(t *testing.T) {
	root := presetTestRoot(t)
	if err := os.Symlink(filepath.Join(root, "mcp.json"), filepath.Join(root, "presets/link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPresets(root, "model"); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlink must fail: %v", err)
	}
	root = t.TempDir()
	writePresetTestFile(t, root, "mcp.json", "[]")
	for _, dir := range []string{"skills", "presets"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	agents, err := LoadPresets(root, "model")
	if err != nil || len(agents) != 0 {
		t.Fatalf("empty catalog must allow retiring all presets: %+v %v", agents, err)
	}
}
