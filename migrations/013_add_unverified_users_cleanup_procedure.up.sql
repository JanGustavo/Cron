-- Stored Procedure to delete a specific unverified user by ID if expired (24 hours)
CREATE OR REPLACE PROCEDURE pr_remove_unverified_user_by_id(p_user_id UUID)
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM users
    WHERE id = p_user_id
      AND is_verified = FALSE
      AND created_at < NOW() - INTERVAL '24 hours';
END;
$$;

-- Stored Procedure to clean up all unverified users that registered more than 24 hours ago
CREATE OR REPLACE PROCEDURE pr_cleanup_expired_unverified_users()
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM users
    WHERE is_verified = FALSE
      AND created_at < NOW() - INTERVAL '24 hours';
END;
$$;

-- Trigger function that calls the cleanup procedure to free up email and CPF
CREATE OR REPLACE FUNCTION fn_trigger_unverified_users_cleanup()
RETURNS TRIGGER AS $$
BEGIN
    -- Execute cleanup of any expired unverified users
    CALL pr_cleanup_expired_unverified_users();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to run cleanup automatically on new user registration (INSERT)
CREATE OR REPLACE TRIGGER trg_cleanup_unverified_users_on_insert
    AFTER INSERT ON users
    FOR EACH STATEMENT
    EXECUTE FUNCTION fn_trigger_unverified_users_cleanup();
