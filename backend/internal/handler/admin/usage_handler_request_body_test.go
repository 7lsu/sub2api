package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminUsageRequestBodyRepoStub struct {
	service.UsageLogRepository
	bodies map[int64]string
	err    error
}

func (s *adminUsageRequestBodyRepoStub) GetRequestBody(_ context.Context, id int64) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	body, ok := s.bodies[id]
	return body, ok, nil
}

func (s *adminUsageRequestBodyRepoStub) ListWithFiltersForAdmin(_ context.Context, params pagination.PaginationParams, _ usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	leaked := "SHOULD-NOT-LEAK"
	logs := []service.UsageLog{{ID: 1, HasRequestBody: true, RequestBody: &leaked}, {ID: 2}}
	return logs, &pagination.PaginationResult{Total: 2, Page: params.Page, PageSize: params.PageSize, Pages: 1}, nil
}

func newAdminUsageRequestBodyRouter(repo *adminUsageRequestBodyRepoStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.GET("/admin/usage", handler.List)
	router.GET("/admin/usage/stats", handler.Stats)
	router.GET("/admin/usage/:id/request-body", handler.GetRequestBody)
	return router
}

func serveAdminUsage(router *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestAdminUsageGetRequestBodyWritesRawText(t *testing.T) {
	body := `{"model":"claude","messages":[{"role":"user","content":"引号\"与换行\n"}]}`
	router := newAdminUsageRequestBodyRouter(&adminUsageRequestBodyRepoStub{bodies: map[int64]string{42: body}})

	rec := serveAdminUsage(router, "/admin/usage/42/request-body")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, body, rec.Body.String(), "body must be written verbatim, not JSON-wrapped")
	require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestAdminUsageGetRequestBodyMissingReturnsNoContent(t *testing.T) {
	router := newAdminUsageRequestBodyRouter(&adminUsageRequestBodyRepoStub{})

	rec := serveAdminUsage(router, "/admin/usage/7/request-body")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rec.Body.String())
}

func TestAdminUsageGetRequestBodyRejectsInvalidID(t *testing.T) {
	router := newAdminUsageRequestBodyRouter(&adminUsageRequestBodyRepoStub{})

	for _, id := range []string{"abc", "0", "-1"} {
		rec := serveAdminUsage(router, "/admin/usage/"+id+"/request-body")
		require.Equal(t, http.StatusBadRequest, rec.Code, id)
	}
}

func TestAdminUsageGetRequestBodyRepoError(t *testing.T) {
	router := newAdminUsageRequestBodyRouter(&adminUsageRequestBodyRepoStub{err: errors.New("boom")})

	rec := serveAdminUsage(router, "/admin/usage/42/request-body")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// fork: admin 列表只给 has_request_body，绝不内联 request_body（2026-11-05 宕机根因）。
func TestAdminUsageListExposesBodyFlagOnly(t *testing.T) {
	router := newAdminUsageRequestBodyRouter(&adminUsageRequestBodyRepoStub{})

	rec := serveAdminUsage(router, "/admin/usage")

	require.Equal(t, http.StatusOK, rec.Code)
	out := rec.Body.String()
	require.NotContains(t, out, `"request_body"`)
	require.NotContains(t, out, "SHOULD-NOT-LEAK")
	require.Equal(t, 1, strings.Count(out, `"has_request_body":true`))
	require.Equal(t, 1, strings.Count(out, `"has_request_body":false`))
}
