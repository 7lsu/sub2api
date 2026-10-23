//go:build unit

package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: Anthropic 账号的 chat/completions 与 responses 兼容入口要把入站请求体写进 usage log，
// 正常收尾与上游未正常结束（转发报错但已有用量）两个计费点都要带上。
func TestGatewayCompatibilityHandlers_UsageLogCapturesRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"chat/completions", "responses"} {
		for _, tc := range []struct {
			name       string
			completion string
		}{
			{name: "complete", completion: compatStreamUsageCompletion},
			{name: "forward_failed_after_output"},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				run := runCompatStreamUsageHandler(t, endpoint, compatStreamUsagePartialStream+tc.completion, tc.completion != "")

				require.Len(t, run.usageRepo.created, 1)
				log := <-run.usageRepo.created
				require.NotNil(t, log.RequestBody)
				require.Contains(t, *log.RequestBody, `"model":"claude-sonnet-4.5"`)
			})
		}
	}
}
