-- Migration: 009 — Adiciona timezone, digest_hour e last_digest_sent_at ao usuário
ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone VARCHAR(50) NOT NULL DEFAULT 'America/Sao_Paulo';
ALTER TABLE users ADD COLUMN IF NOT EXISTS digest_hour INT NOT NULL DEFAULT 18;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_digest_sent_at TIMESTAMPTZ;
