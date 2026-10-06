ALTER TABLE users ADD COLUMN avatar_url TEXT;
ALTER TABLE users ADD CONSTRAINT users_avatar_url_length CHECK (avatar_url IS NULL OR length(avatar_url) <= 2048);
