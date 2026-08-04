-- Migration: 004 — Adiciona next_job_id e tags (Down)
ALTER TABLE jobs DROP COLUMN next_job_id;
ALTER TABLE jobs DROP COLUMN tags;
