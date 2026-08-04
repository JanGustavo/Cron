-- Migration: 004 — Adiciona next_job_id e tags
ALTER TABLE jobs ADD COLUMN next_job_id UUID REFERENCES jobs(id) ON DELETE SET NULL;
ALTER TABLE jobs ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';
