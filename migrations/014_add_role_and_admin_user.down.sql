-- Down Migration 014
DELETE FROM users WHERE email = 'jandersongustavo1@gmail.com';
ALTER TABLE users DROP COLUMN IF EXISTS role;
