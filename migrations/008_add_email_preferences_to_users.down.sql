-- Rollback: 008 — Remove preferências de e-mail do usuário
ALTER TABLE users DROP COLUMN email_alerts_enabled;
ALTER TABLE users DROP COLUMN daily_digest_enabled;
