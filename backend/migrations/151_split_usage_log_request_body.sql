-- 151_split_usage_log_request_body.sql
-- 将 usage_logs.request_body 拆分到按天 RANGE 分区的独立表 usage_log_request_bodies，
-- 并删除主表 request_body 列。旧 request_body 数据按决策直接丢弃，不回填、不保留。
-- 单文件单事务：建表与 DROP 列原子完成（自研 runner 普通 .sql 在事务内执行）。

-- (1) 分区父表 + DEFAULT 兜底分区。
-- 关联/分区键 (request_id, api_key_id, created_at)；request_id 与主表同为 VARCHAR(64)。
CREATE TABLE IF NOT EXISTS usage_log_request_bodies (
    request_id   VARCHAR(64)  NOT NULL,
    api_key_id   BIGINT       NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL,
    request_body TEXT         NOT NULL,
    PRIMARY KEY (request_id, api_key_id, created_at)
) PARTITION BY RANGE (created_at);

-- 兜底分区：维护任务停摆导致目标日分区缺失时接住写入，避免插入失败。
-- 正常情况下维护任务会提前建好按天分区，数据不会落到此处。
CREATE TABLE IF NOT EXISTS usage_log_request_bodies_default
    PARTITION OF usage_log_request_bodies DEFAULT;

-- (2) 初始按天分区：昨天 .. 今天+7 天（覆盖部署到首次维护任务运行之间的窗口）。
-- 分区边界统一按 UTC 取整天，与迁移 035 保持一致。
DO $$
DECLARE
    d DATE;
BEGIN
    FOR d IN
        SELECT generate_series(
            (date_trunc('day', now() AT TIME ZONE 'UTC')::date - INTERVAL '1 day'),
            (date_trunc('day', now() AT TIME ZONE 'UTC')::date + INTERVAL '7 day'),
            INTERVAL '1 day'
        )::date
    LOOP
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS usage_log_request_bodies_%s PARTITION OF usage_log_request_bodies FOR VALUES FROM (%L) TO (%L)',
            to_char(d, 'YYYYMMDD'),
            d,
            (d + INTERVAL '1 day')::date
        );
    END LOOP;
END $$;

-- (3) 删除主表 request_body 列 + 迁移 140 建的清理索引（已无意义）。
-- DROP COLUMN 在分区父表上级联到所有分区，为瞬时元数据操作；旧 body 数据随之丢弃。
ALTER TABLE usage_logs DROP COLUMN IF EXISTS request_body;
DROP INDEX IF EXISTS idx_usage_logs_request_body_cleanup_created;
