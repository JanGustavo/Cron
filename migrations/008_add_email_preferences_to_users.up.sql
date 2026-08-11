-- Migration: 008 — Adiciona preferências de e-mail ao usuário
ALTER TABLE users ADD COLUMN email_alerts_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN daily_digest_enabled BOOLEAN NOT NULL DEFAULT FALSE;
