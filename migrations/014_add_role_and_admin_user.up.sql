-- Migration 014: Add role column to users and seed initial ADM user
ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'user';

-- Insert or Update ADM User (jandersongustavo1@gmail.com) with role='admin', plan='pro', is_verified=TRUE
INSERT INTO users (
    id,
    email,
    password_hash,
    plan,
    full_name,
    is_verified,
    role,
    created_at
) VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'jandersongustavo1@gmail.com',
    '$2a$10$bgJ4mHedhX1XgwqWm9vSh.2yrE1YVOo4aR7dpIm615fihAAO3SRdW',
    'pro',
    'Janderson Gustavo (ADM)',
    TRUE,
    'admin',
    NOW()
) ON CONFLICT (email) DO UPDATE SET
    role = 'admin',
    plan = 'pro',
    is_verified = TRUE,
    password_hash = '$2a$10$bgJ4mHedhX1XgwqWm9vSh.2yrE1YVOo4aR7dpIm615fihAAO3SRdW';

-- Ensure ADM user has a default project workspace
INSERT INTO projects (
    id,
    user_id,
    name,
    created_at
) VALUES (
    'b0000000-0000-0000-0000-000000000001',
    'a0000000-0000-0000-0000-000000000001',
    'Workspace ADM Principal',
    NOW()
) ON CONFLICT (id) DO NOTHING;
