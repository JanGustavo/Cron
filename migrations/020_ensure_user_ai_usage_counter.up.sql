-- Profile reads already depend on this counter. Some deployed databases had it
-- added outside migrations; keep existing values and support fresh databases.
ALTER TABLE users ADD COLUMN IF NOT EXISTS ai_queries_used INTEGER NOT NULL DEFAULT 0;
