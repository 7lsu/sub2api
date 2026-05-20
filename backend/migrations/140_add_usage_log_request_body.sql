-- Add full request body capture to usage logs.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS request_body TEXT;

-- Speed up the fixed retention worker that clears usage request bodies.
CREATE INDEX IF NOT EXISTS idx_usage_logs_request_body_cleanup_created
    ON usage_logs (created_at, id)
    WHERE request_body IS NOT NULL;
