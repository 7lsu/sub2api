//go:build unit

package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// fork: Key 并发上限只允许管理员设置，用户侧接口必须忽略 concurrency_limit。
func TestUserAPIKeyRoutesCannotWriteConcurrencyLimit(t *testing.T) {
	jsonHeaders := map[string]string{"Content-Type": "application/json"}

	t.Run("create ignores concurrency_limit", func(t *testing.T) {
		deps := newContractDeps(t)
		status, body := doRequest(t, deps.router, http.MethodPost, "/api/v1/keys",
			`{"name":"Key One","custom_key":"sk_custom_1234567890","concurrency_limit":5}`, jsonHeaders)
		require.Equal(t, http.StatusOK, status, body)

		var resp struct {
			Data struct {
				ID               int64 `json:"id"`
				ConcurrencyLimit int   `json:"concurrency_limit"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		require.Zero(t, resp.Data.ConcurrencyLimit)
		require.Zero(t, deps.apiKeyRepo.byID[resp.Data.ID].ConcurrencyLimit)
	})

	t.Run("update cannot clear admin limit", func(t *testing.T) {
		deps := newContractDeps(t)
		deps.apiKeyRepo.MustSeed(&service.APIKey{
			ID:               100,
			UserID:           1,
			Key:              "sk_custom_1234567890",
			Name:             "Key One",
			Status:           service.StatusActive,
			ConcurrencyLimit: 3,
			CreatedAt:        deps.now,
			UpdatedAt:        deps.now,
		})

		status, body := doRequest(t, deps.router, http.MethodPut, "/api/v1/keys/100",
			`{"name":"renamed","concurrency_limit":0}`, jsonHeaders)
		require.Equal(t, http.StatusOK, status, body)

		stored := deps.apiKeyRepo.byID[100]
		require.Equal(t, "renamed", stored.Name)
		require.Equal(t, 3, stored.ConcurrencyLimit)
	})
}
