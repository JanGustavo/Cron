-- Seed default user
INSERT INTO users (id, email, plan)
VALUES ('e77aafaf-16bf-4c03-8e6f-40393c7b0a90', 'user@cronflow.com', 'free')
ON CONFLICT DO NOTHING;

-- Seed default project
INSERT INTO projects (id, user_id, name)
VALUES ('777aafaf-16bf-4c03-8e6f-40393c7b0a90', 'e77aafaf-16bf-4c03-8e6f-40393c7b0a90', 'My Cronflow Project')
ON CONFLICT DO NOTHING;

-- Seed API Key "cf_live_test_key"
-- SHA-256 hash of "cf_live_test_key" is: f1d7c64092eb340d12b84a62cb12d8e7b36dda602fba8150e2129ef8d07fe278
INSERT INTO api_keys (id, project_id, key_hash, prefix)
VALUES ('999aafaf-16bf-4c03-8e6f-40393c7b0a90', '777aafaf-16bf-4c03-8e6f-40393c7b0a90', 'f1d7c64092eb340d12b84a62cb12d8e7b36dda602fba8150e2129ef8d07fe278', 'cf_live_test_k')
ON CONFLICT DO NOTHING;

-- Seed a test job with ID "555aafaf-16bf-4c03-8e6f-40393c7b0a91"
INSERT INTO jobs (id, project_id, name, schedule, timezone, url, http_method, headers, payload, status, next_run_at, consecutive_failures, webhook_alert_url)
VALUES ('555aafaf-16bf-4c03-8e6f-40393c7b0a91', '777aafaf-16bf-4c03-8e6f-40393c7b0a90', 'Test Job', '*/1 * * * *', 'UTC', 'https://httpbin.org/status/200', 'POST', '{"Content-Type": "application/json"}', '{"hello": "world"}', 'active', NOW() + INTERVAL '1 minute', 0, NULL)
ON CONFLICT DO NOTHING;
