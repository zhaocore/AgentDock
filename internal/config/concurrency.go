package config

import (
	"fmt"
	"strconv"

	"github.com/spf13/viper"
)

// ConcurrencyConfig 保存单进程执行限额，修改后重启生效。
type ConcurrencyConfig struct {
	Worker  int `json:"worker"`
	Capture int `json:"capture"`
	Task    int `json:"task"`
	Report  int `json:"report"`
}

// loadConcurrency 拒绝非法值，避免零值阻塞与隐式类型转换。
func loadConcurrency() (ConcurrencyConfig, error) {
	keys := []string{"WORKER_CONCURRENCY", "CRAWL_CAPTURE_CONCURRENCY", "CRAWL_TASK_CONCURRENCY", "CRAWL_REPORT_CONCURRENCY"}
	defaults := []int{4, 3, 3, 1}
	values := make([]int, len(keys))
	for i, key := range keys {
		viper.SetDefault(key, defaults[i])
		value, err := strconv.Atoi(viper.GetString(key))
		if err != nil || value < 1 || value > 64 {
			return ConcurrencyConfig{}, fmt.Errorf("%s must be an integer between 1 and 64", key)
		}
		values[i] = value
	}
	return ConcurrencyConfig{Worker: values[0], Capture: values[1], Task: values[2], Report: values[3]}, nil
}
