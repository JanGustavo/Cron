-- Migration: 007 (Down) — Remove coluna webhook_secret na tabela de projetos
ALTER TABLE projects DROP COLUMN IF EXISTS webhook_secret;
