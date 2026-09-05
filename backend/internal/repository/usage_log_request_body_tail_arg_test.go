package repository

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// fork: request_body 不入 usage_logs 主表，作为最后一个参数传给写分区子表
// usage_log_request_bodies 的 CTE。上游每加一列 usage_logs，这个尾参的编号
// 就整体后移一位——同步 upstream 时最容易静默错位的地方（自动合并可能保留旧
// 的 $N::text，而 $N 已经变成别的列）。以下用例把「尾参必须在最后、且两条静态
// SQL 都引用同一个编号」钉死。

const usageLogColumnCount = 62 // usage_logs 实际列数，不含 fork 尾参

func newTailArgUsageLog(body *string) *service.UsageLog {
	return &service.UsageLog{
		UserID: 1, APIKeyID: 2, AccountID: 3,
		RequestID: "req-tail-arg", Model: "m", RequestedModel: "m",
		RequestBody: body,
		CreatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestUsageLogRequestBodyIsLastPreparedArg(t *testing.T) {
	body := "BODY"
	prepared := prepareUsageLogInsert(newTailArgUsageLog(&body))

	require.Len(t, usageLogInsertArgTypes, usageLogColumnCount+1,
		"arg-type 表 = usage_logs 列数 + fork request_body 尾参")
	require.Len(t, prepared.args, len(usageLogInsertArgTypes))

	tail, ok := prepared.args[len(prepared.args)-1].(sql.NullString)
	require.True(t, ok, "尾参类型应为 sql.NullString，实际 %T", prepared.args[len(prepared.args)-1])
	require.True(t, tail.Valid)
	require.Equal(t, body, tail.String, "最后一个参数必须是 request_body")
}

var usageLogInputListRe = regexp.MustCompile(`(?s)WITH input \(\s*(.*?)\s*\) AS \(VALUES`)
var usageLogColumnRe = regexp.MustCompile(`[a-z_0-9]+`)

// 批量与 best-effort 两条路径的列清单由手写字符串拼接，顺序必须与 prepared.args 一致。
func TestUsageLogBatchQueriesKeepRequestBodyLast(t *testing.T) {
	body := "BODY"
	log := newTailArgUsageLog(&body)
	prepared := prepareUsageLogInsert(log)
	key := usageLogBatchKey(log.RequestID, log.APIKeyID)

	batchQuery, batchArgs := buildUsageLogBatchInsertQuery([]string{key},
		map[string]usageLogInsertPrepared{key: prepared})
	require.Len(t, batchArgs, len(prepared.args)+1, "batch 多一个 input_idx")
	batchCols := assertUsageLogInputList(t, "batch", batchQuery)
	require.Equal(t, "input_idx", batchCols[0])

	bestEffortQuery, bestEffortArgs := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	require.Len(t, bestEffortArgs, len(prepared.args))
	assertUsageLogInputList(t, "best-effort", bestEffortQuery)
}

// assertUsageLogInputList 校验 CTE 输入列表尾部四列顺序，返回完整列名切片。
func assertUsageLogInputList(t *testing.T, name, query string) []string {
	t.Helper()
	m := usageLogInputListRe.FindStringSubmatch(query)
	require.NotNil(t, m, "%s: 找不到 WITH input (...) 列清单", name)
	cols := usageLogColumnRe.FindAllString(m[1], -1)

	tail := cols[len(cols)-4:]
	require.Equal(t, []string{"session_id", "native_compaction_v2", "created_at", "request_body"}, tail,
		"%s: CTE 输入列尾部顺序必须与 usageLogInsertArgTypes 一致", name)
	return cols
}

// createSingle / execUsageLogInsertNoResult 两条静态 SQL 里 body_ins 引用的占位符
// 必须等于尾参编号；引用错列时 sqlmock 的正则期望不匹配，用例直接失败。
func TestUsageLogStaticQueriesReferenceRequestBodyTailPlaceholder(t *testing.T) {
	placeholder := `\$` + strconv.Itoa(usageLogColumnCount+1) + `::text`
	body := "BODY"
	createdAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("createSingle", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(placeholder).
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(1), createdAt))

		_, err := repo.createSingle(context.Background(), db, newTailArgUsageLog(&body))
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("execUsageLogInsertNoResult", func(t *testing.T) {
		db, mock := newSQLMock(t)
		mock.ExpectExec(placeholder).WillReturnResult(sqlmock.NewResult(0, 1))

		err := execUsageLogInsertNoResult(context.Background(), db,
			prepareUsageLogInsert(newTailArgUsageLog(&body)))
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
