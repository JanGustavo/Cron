-- Migration: 009 — Remove timezone, digest_hour e last_digest_sent_at do usuário
ALTER TABLE users DROP COLUMN IF EXISTS timezone;
ALTER TABLE users DROP COLUMN IF EXISTS digest_hour;
ALTER TABLE users DROP COLUMN IF EXISTS last_digest_sent_at;
