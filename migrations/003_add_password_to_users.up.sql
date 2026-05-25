-- Migration: 003 — Adiciona coluna password_hash na tabela users
ALTER TABLE users ADD COLUMN password_hash TEXT;
