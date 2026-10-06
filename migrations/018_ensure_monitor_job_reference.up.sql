-- Some installations added job_id before migration 016 without its foreign key.
-- Preserve any historical orphan rows; enforce future writes and job deletion.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY(c.conkey)
        WHERE c.conrelid = 'monitor_rules'::regclass
          AND c.confrelid = 'jobs'::regclass
          AND c.contype = 'f' AND a.attname = 'job_id'
    ) THEN
        ALTER TABLE monitor_rules ADD CONSTRAINT monitor_rules_job_reference_fkey
            FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE NOT VALID;
    END IF;
END $$;
