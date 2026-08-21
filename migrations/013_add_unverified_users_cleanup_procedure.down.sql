DROP TRIGGER IF EXISTS trg_cleanup_unverified_users_on_insert ON users;
DROP FUNCTION IF EXISTS fn_trigger_unverified_users_cleanup();
DROP PROCEDURE IF EXISTS pr_cleanup_expired_unverified_users();
DROP PROCEDURE IF EXISTS pr_remove_unverified_user_by_id(UUID);
