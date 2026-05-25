-- Migration: 003 — Remove coluna password_hash da tabela users
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
