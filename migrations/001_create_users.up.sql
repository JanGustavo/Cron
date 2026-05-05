-- Migration: 001 — Usuários e autenticação
-- Cria as tabelas fundamentais de identidade da plataforma.

CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- para gen_random_uuid()

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL UNIQUE,
    plan       TEXT NOT NULL DEFAULT 'free', -- 'free' | 'paid'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE projects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE api_keys (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key_hash   TEXT NOT NULL UNIQUE, -- SHA-256 da API Key. NUNCA o plain text.
    prefix     TEXT NOT NULL,        -- Ex: "cf_live_abc123" para identificação em logs
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
