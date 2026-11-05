package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// fork: admin 列表只能带 has_request_body 标记，不能把 request_body 本身拉出来（2026-11-05 一页 609MB 撑爆容器）。
func TestAdminUsageListQueryNeverSelectsRequestBody(t *testing.T) {
	query := buildUsageLogListQuery(true, "WHERE api_key_id = $1", "created_at DESC, id DESC", 2, 3)

	require.Contains(t, query, "EXISTS (SELECT 1 FROM usage_log_request_bodies b")
	require.Contains(t, query, "AS has_request_body")
	require.NotContains(t, query, "b.request_body")
	require.NotContains(t, query, "JOIN usage_log_request_bodies")

	userQuery := buildUsageLogListQuery(false, "WHERE api_key_id = $1", "id DESC", 2, 3)
	require.NotContains(t, userQuery, "usage_log_request_bodies")
}

type bodyFlagScanner struct {
	destCount int
	flag      bool
}

func (s *bodyFlagScanner) Scan(dest ...any) error {
	s.destCount = len(dest)
	if p, ok := dest[len(dest)-1].(*bool); ok {
		*p = s.flag
	}
	return nil
}

func TestScanUsageLogWithBodyFlagReadsTrailingFlag(t *testing.T) {
	columns := strings.Count(usageLogSelectColumns, ",") + 1

	plain := &bodyFlagScanner{flag: true}
	log, err := scanUsageLogWithBodyFlag(plain, false)
	require.NoError(t, err)
	require.Equal(t, columns, plain.destCount)
	require.False(t, log.HasRequestBody)

	admin := &bodyFlagScanner{flag: true}
	log, err = scanUsageLogWithBodyFlag(admin, true)
	require.NoError(t, err)
	require.Equal(t, columns+1, admin.destCount)
	require.True(t, log.HasRequestBody)
	require.Nil(t, log.RequestBody)
}

func TestUsageLogRepositoryGetRequestBody(t *testing.T) {
	const query = `SELECT b\.request_body FROM usage_logs u JOIN usage_log_request_bodies b .* WHERE u\.id = \$1`

	t.Run("found", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(query).WithArgs(int64(42)).
			WillReturnRows(sqlmock.NewRows([]string{"request_body"}).AddRow(`{"model":"claude"}`))

		body, found, err := repo.GetRequestBody(context.Background(), 42)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, `{"model":"claude"}`, body)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not captured or partition dropped", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(query).WithArgs(int64(7)).
			WillReturnRows(sqlmock.NewRows([]string{"request_body"}))

		body, found, err := repo.GetRequestBody(context.Background(), 7)
		require.NoError(t, err)
		require.False(t, found)
		require.Empty(t, body)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(query).WithArgs(int64(7)).WillReturnError(errors.New("boom"))

		_, found, err := repo.GetRequestBody(context.Background(), 7)
		require.Error(t, err)
		require.False(t, found)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
