-- Migration: 007 — Adiciona coluna webhook_secret na tabela de projetos
ALTER TABLE projects ADD COLUMN IF NOT EXISTS webhook_secret TEXT;
