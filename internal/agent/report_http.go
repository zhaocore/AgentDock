package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/zhaocore/dutybotgo/internal/telemetry"
)

// reportTransport 对模型临时错误有限重试，避免服务错误正文泄露采集内容。
type reportTransport struct{ base http.RoundTripper }

// NewReportRuntime 为后台报告配置独立超时、重试和不跟随重定向的模型客户端。
func NewReportRuntime(key, base string) *LangChainRuntime {
	return &LangChainRuntime{apiKey: key, baseURL: base, httpClient: &http.Client{Timeout: 90 * time.Second, Transport: reportTransport{base: http.DefaultTransport}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("report provider redirect rejected") }}}
}

// RoundTrip 每次重建请求体，只重试网络、限流和服务器错误。
func (t reportTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; attempt < 3; attempt++ {
		copy := req.Clone(req.Context())
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			copy.Body = body
		}
		started := time.Now()
		response, err := t.base.RoundTrip(copy)
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		telemetry.Count(req.Context(), fmt.Sprintf("model_http_status_%d", status), 1)
		measurementErr := err
		if err == nil && (status < 200 || status >= 300) {
			measurementErr = fmt.Errorf("provider status %d", status)
		}
		telemetry.Measure(req.Context(), "model_http_headers", started, measurementErr)
		if err == nil && status >= 200 && status < 300 {
			return response, nil
		}
		if response != nil {
			response.Body.Close()
		}
		last := fmt.Errorf("report provider request failed status=%d; check provider availability, model and credentials", status)
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if attempt == 2 || (err == nil && status != 429 && status < 500) {
			return nil, last
		}
		telemetry.Count(req.Context(), "model_retries", 1)
		slog.Warn("retry report provider", "run_id", telemetry.RunID(req.Context()), "attempt", attempt+1, "status", status)
		backoff := time.Now()
		err = reportDelay(req.Context(), time.Duration(attempt+1)*time.Second)
		telemetry.Measure(req.Context(), "model_backoff", backoff, err)
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("report provider attempts exhausted")
}

// reportDelay 在取消时立即终止重试等待。
func reportDelay(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
