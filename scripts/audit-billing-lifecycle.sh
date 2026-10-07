#!/usr/bin/env bash
# Every invocation creates and deletes its own local cluster. No production DSN.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
audit_pg_bin="${CRONFLOW_PG_BIN:-/usr/lib/postgresql/16/bin}"
audit_cluster_dir=$(mktemp -d /tmp/cronflow-billing-pg.XXXXXX)
audit_cleanup() {
 "$audit_pg_bin/pg_ctl" -D "$audit_cluster_dir/data" -m immediate stop >/dev/null 2>&1 || true
 rm -rf "$audit_cluster_dir"
}
trap audit_cleanup EXIT
"$audit_pg_bin/initdb" -D "$audit_cluster_dir/data" --auth=trust --no-locale >/dev/null
"$audit_pg_bin/pg_ctl" -D "$audit_cluster_dir/data" -l "$audit_cluster_dir/server.log" -o "-k $audit_cluster_dir -p 55447 -h ''" start >/dev/null
for audit_migration in migrations/*.up.sql; do
 "$audit_pg_bin/psql" -h "$audit_cluster_dir" -p 55447 -d postgres -v ON_ERROR_STOP=1 -q -f "$audit_migration" >/dev/null
done
# Verify the new additive policy migration can be rolled back and reapplied.
"$audit_pg_bin/psql" -h "$audit_cluster_dir" -p 55447 -d postgres -v ON_ERROR_STOP=1 -q -f migrations/021_job_downgrade_grace.down.sql >/dev/null
"$audit_pg_bin/psql" -h "$audit_cluster_dir" -p 55447 -d postgres -v ON_ERROR_STOP=1 -q -f migrations/021_job_downgrade_grace.up.sql >/dev/null
"$audit_pg_bin/psql" -h "$audit_cluster_dir" -p 55447 -d postgres -v ON_ERROR_STOP=1 -q -f migrations/022_asaas_reconciliation.down.sql >/dev/null
"$audit_pg_bin/psql" -h "$audit_cluster_dir" -p 55447 -d postgres -v ON_ERROR_STOP=1 -q -f migrations/022_asaas_reconciliation.up.sql >/dev/null
CRONFLOW_MONITOR_TEST_DSN="host=$audit_cluster_dir port=55447 dbname=postgres sslmode=disable" \
go test -race ./internal/repository/postgres -run 'TestJobDowngradeGraceIntegration|TestAsaasReconciliationIntegration|TestLogRetentionMatchesAccountEntitlements|TestAvatarProfileIntegration|TestMonitorPersistenceIntegration' -count=1 -v
