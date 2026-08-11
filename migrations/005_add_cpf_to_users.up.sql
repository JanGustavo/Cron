-- Migration: 005 — Adiciona CPF e nome completo únicos aos usuários
ALTER TABLE users ADD COLUMN cpf VARCHAR(11) UNIQUE;
ALTER TABLE users ADD COLUMN full_name TEXT;
