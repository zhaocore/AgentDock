package config

import "testing"

func TestLoadAgentDirectory(t *testing.T) {
	t.Setenv("AGENT_CONFIG_DIR", "/srv/agent-config")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.RootDir != "/srv/agent-config" {
		t.Fatalf("agent root=%s", cfg.Agent.RootDir)
	}
}
