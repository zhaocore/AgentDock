package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestConcurrencyEnvironment(t *testing.T) {
	keys := []string{"WORKER_CONCURRENCY", "CRAWL_CAPTURE_CONCURRENCY", "CRAWL_TASK_CONCURRENCY", "CRAWL_REPORT_CONCURRENCY"}
	t.Chdir(t.TempDir())
	viper.Reset()
	t.Cleanup(viper.Reset)
	for _, key := range keys {
		t.Setenv(key, "3")
	}
	cfg, err := Load()
	if err != nil || cfg.Concurrency != (ConcurrencyConfig{Worker: 3, Capture: 3, Task: 3, Report: 3}) {
		t.Fatal(cfg, err)
	}
	for _, key := range keys {
		for _, value := range []string{"0", "-1", "65", "1.5", "wrong", "99999999999999999999999999"} {
			t.Run(key+value, func(t *testing.T) {
				t.Setenv(key, value)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
					t.Fatal(err)
				}
			})
		}
	}
	for _, key := range keys {
		t.Setenv(key, "64")
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrencyDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	viper.Reset()
	t.Cleanup(viper.Reset)
	// 空环境项沿用 Viper 的未配置语义。
	for _, key := range []string{"WORKER_CONCURRENCY", "CRAWL_CAPTURE_CONCURRENCY", "CRAWL_TASK_CONCURRENCY", "CRAWL_REPORT_CONCURRENCY"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil || cfg.Concurrency != (ConcurrencyConfig{Worker: 4, Capture: 3, Task: 3, Report: 1}) {
		t.Fatal(cfg, err)
	}
}
