-- A single account policy for provider events, admin changes and period expiry.
ALTER TABLE users ADD COLUMN job_grace_ends_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN job_grace_active_cap INT;
ALTER TABLE users ADD COLUMN pro_ended_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN pause_reason TEXT;

CREATE FUNCTION cronflow_effective_plan(account_id UUID, at_time TIMESTAMPTZ DEFAULT NOW()) RETURNS TEXT
LANGUAGE SQL STABLE AS $$
 SELECT CASE
  WHEN s.status IN ('active','trialing') AND (s.current_period_end IS NULL OR s.current_period_end>at_time) THEN s.plan_code
  WHEN s.id IS NULL AND u.plan='pro' THEN 'pro'
  ELSE 'free' END
 FROM users u LEFT JOIN subscriptions s ON s.user_id=u.id WHERE u.id=account_id
$$;

CREATE FUNCTION cronflow_reconcile_account(account_id UUID, at_time TIMESTAMPTZ DEFAULT NOW()) RETURNS VOID
LANGUAGE plpgsql AS $$
DECLARE effective TEXT; previous TEXT; active_count INT; free_cap INT; deadline TIMESTAMPTZ;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtext(account_id::TEXT));
 SELECT plan INTO previous FROM users WHERE id=account_id;
 IF NOT FOUND THEN RETURN; END IF;
 effective:=cronflow_effective_plan(account_id,at_time);
 SELECT max_jobs INTO free_cap FROM plans WHERE code='free';
 IF effective<>'free' THEN
  UPDATE users SET plan=effective,job_grace_ends_at=NULL,job_grace_active_cap=NULL,pro_ended_at=NULL WHERE id=account_id;
  RETURN;
 END IF;
 UPDATE users SET plan='free',pro_ended_at=CASE WHEN previous<>'free' THEN COALESCE(pro_ended_at,at_time) ELSE pro_ended_at END WHERE id=account_id;
 SELECT count(*) INTO active_count FROM jobs j JOIN projects p ON p.id=j.project_id WHERE p.user_id=account_id AND j.status='active';
 SELECT job_grace_ends_at INTO deadline FROM users WHERE id=account_id;
 IF deadline IS NULL AND active_count>free_cap THEN
  deadline:=at_time+INTERVAL '48 hours';
  UPDATE users SET job_grace_ends_at=deadline,job_grace_active_cap=active_count WHERE id=account_id;
 END IF;
 IF deadline IS NOT NULL AND deadline<=at_time THEN
  WITH ranked AS (
   SELECT j.id,row_number() OVER(ORDER BY j.created_at ASC,j.id ASC) AS n
   FROM jobs j JOIN projects p ON p.id=j.project_id WHERE p.user_id=account_id AND j.status='active'
  ) UPDATE jobs SET status='paused',pause_reason='plan_limit',updated_at=at_time WHERE id IN(SELECT id FROM ranked WHERE n>free_cap);
 END IF;
END $$;

CREATE FUNCTION cronflow_subscription_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 PERFORM cronflow_reconcile_account(NEW.user_id);
 RETURN NEW;
END $$;
CREATE TRIGGER subscription_account_policy AFTER INSERT OR UPDATE ON subscriptions
FOR EACH ROW EXECUTE FUNCTION cronflow_subscription_changed();
