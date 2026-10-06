package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/domain/monitor"
)

type historyConnector struct{ payload driver.Value }

func (c historyConnector) Connect(context.Context) (driver.Conn, error) {
	return &historyConn{payload: c.payload}, nil
}
func (historyConnector) Driver() driver.Driver { return historyDriver{} }

type historyDriver struct{}

func (historyDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type historyConn struct{ payload driver.Value }

func (*historyConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*historyConn) Close() error              { return nil }
func (*historyConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected write") }
func (c *historyConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "COUNT(e.id)") {
		return &historyRows{columns: []string{"count"}, values: []driver.Value{int64(1)}}, nil
	}
	columns := []string{"id", "job_id", "status", "http_status", "duration_ms", "response_body", "attempt_number", "triggered_at", "rule_evaluations", "transport_status"}
	values := []driver.Value{"execution", "job", "success", int64(200), int64(20), `{"success":false}`, int64(1), time.Now(), c.payload, "success"}
	if strings.Contains(query, "j.name as job_name") {
		columns = append(columns, "job_name", "job_url")
		values = append(values, "Job", "https://example.com")
	}
	return &historyRows{columns: columns, values: values}, nil
}

type historyRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (r *historyRows) Columns() []string { return r.columns }
func (*historyRows) Close() error        { return nil }
func (r *historyRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.values)
	r.done = true
	return nil
}

func TestExecutionHistoryExposesEvaluationsAndAcceptsLegacyNull(t *testing.T) {
	for _, payload := range []driver.Value{nil, []byte(`[{"rule_id":"rule","rule_name":"Success","passed":false,"status":"fail","reason":"condition_not_met","observed_value":false}]`)} {
		db := sql.OpenDB(historyConnector{payload: payload})
		repo := NewExecutionRepository(db)
		jobs, err := repo.ListByJob(context.Background(), "job", 10)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("job history failed: %v", err)
		}
		project, total, err := repo.ListByProject(context.Background(), "project", 10, 0, "", "", "", "")
		if err != nil || total != 1 || len(project) != 1 {
			t.Fatalf("project history failed: %v", err)
		}
		for _, raw := range []json.RawMessage{jobs[0].RuleEvaluations, project[0].RuleEvaluations} {
			if payload == nil {
				if len(raw) != 0 {
					t.Fatal("invented evaluations on old execution")
				}
				continue
			}
			var statuses []monitor.RuleStatus
			if err := json.Unmarshal(raw, &statuses); err != nil {
				t.Fatal(err)
			}
			if len(statuses) != 1 || statuses[0].Status != "fail" || string(statuses[0].ObservedValue) != "false" {
				t.Fatalf("evaluation lost: %s", raw)
			}
		}
		db.Close()
	}
}
