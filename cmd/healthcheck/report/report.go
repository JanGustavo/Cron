package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JanGustavo/Cron/cmd/healthcheck/config"
)

type Report struct {
	Timestamp    time.Time       `json:"timestamp"`
	Duration     time.Duration   `json:"duration"`
	BaseURL      string          `json:"base_url"`
	Environment  string          `json:"environment"`
	Total        int             `json:"total"`
	Passed       int             `json:"passed"`
	Failed       int             `json:"failed"`
	Skipped      int             `json:"skipped"`
	Results      []*CheckResult  `json:"results"`
	Summary      Summary         `json:"summary"`
	Diagnostics  Diagnostics     `json:"diagnostics"`
}

type CheckResult struct {
	Name        string                 `json:"name"`
	Category    string                 `json:"category"`
	Status      string                 `json:"status"` // pass, fail, skip
	Duration    time.Duration          `json:"duration"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Endpoint    string                 `json:"endpoint,omitempty"`
	Method      string                 `json:"method,omitempty"`
	RequestBody string                 `json:"request_body,omitempty"`
	Response    string                 `json:"response,omitempty"`
	RunMode     string                 `json:"run_mode"` // production, local, fallback
}

type Summary struct {
	ByCategory    map[string]CategoryStats `json:"by_category"`
	CriticalFails int                      `json:"critical_fails"`
	Warnings      int                      `json:"warnings"`
}

type CategoryStats struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Skipped int `json:"skipped"`
}

type Diagnostics struct {
	ProductionAvailable bool          `json:"production_available"`
	LocalAvailable      bool          `json:"local_available"`
	FallbackUsed        bool          `json:"fallback_used"`
	FallbackResults     []*CheckResult `json:"fallback_results,omitempty"`
}

func (r *Report) HasFailures() bool {
	return r.Failed > 0
}

func (r *Report) AddResult(res *CheckResult) {
	r.Results = append(r.Results, res)
	r.Total++
	switch res.Status {
	case "pass":
		r.Passed++
	case "fail":
		r.Failed++
	case "skip":
		r.Skipped++
	}
}

func NewReport(baseURL string) *Report {
	env := "production"
	if strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") {
		env = "local"
	}
	return &Report{
		BaseURL:     baseURL,
		Environment: env,
		Summary: Summary{
			ByCategory: make(map[string]CategoryStats),
		},
		Diagnostics: Diagnostics{},
	}
}

func Save(cfg *config.Config, r *Report) {
	if err := os.MkdirAll(cfg.Report.Dir, 0755); err != nil {
		fmt.Printf("[Report] Erro criando diretório: %v\n", err)
		return
	}

	ts := r.Timestamp.Format("20060102_150405")
	base := filepath.Join(cfg.Report.Dir, fmt.Sprintf("healthcheck_%s", ts))

	if cfg.Report.JSONEnabled {
		data, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(base+".json", data, 0644); err != nil {
			fmt.Printf("[Report] Erro salvando JSON: %v\n", err)
		}
	}

	if cfg.Report.HTMLEnabled {
		html := generateHTML(r)
		if err := os.WriteFile(base+".html", []byte(html), 0644); err != nil {
			fmt.Printf("[Report] Erro salvando HTML: %v\n", err)
		}
	}

	cleanOld(cfg.Report.Dir, cfg.Report.Retention)
}

func cleanOld(dir string, retention time.Duration) {
	cutoff := time.Now().Add(-retention)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func generateHTML(r *Report) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"><title>Health Check Report</title>
<style>
body{font-family:system-ui,sans-serif;background:#0f172a;color:#e2e8f0;padding:2rem;max-width:1200px;margin:0 auto}
h1{color:#22d3ee;border-bottom:1px solid #1e293b;padding-bottom:1rem}
.summary{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:1rem;margin:1.5rem 0}
.card{background:#1e293b;padding:1rem;border-radius:8px;border:1px solid #334155}
.card.pass{border-color:#10b981}.card.fail{border-color:#ef4444}.card.warn{border-color:#f59e0b}
.card h3{margin:0 0 0.5rem;font-size:0.875rem;color:#94a3b8;text-transform:uppercase}
.card .num{font-size:2rem;font-weight:bold;color:#fff}
table{width:100%;border-collapse:collapse;margin-top:1rem}
th,td{padding:0.75rem;text-align:left;border-bottom:1px solid #334155}
th{background:#1e293b;color:#94a3b8;font-weight:600}
tr:hover{background:#1e293b}
.badge{display:inline-block;padding:0.25rem 0.5rem;border-radius:4px;font-size:0.75rem;font-weight:600}
.badge-pass{background:#10b98120;color:#10b981}
.badge-fail{background:#ef444420;color:#ef4444}
.badge-skip{background:#f59e0b20;color:#f59e0b}
.details{font-family:monospace;font-size:0.75rem;color:#94a3b8;background:#0f172a;padding:0.5rem;border-radius:4px;max-height:200px;overflow:auto}
.meta{color:#64748b;font-size:0.875rem;margin-bottom:1rem}
</style></head><body>
`)
	sb.WriteString(fmt.Sprintf("<h1>🏥 Health Check Report — %s</h1>", r.Timestamp.Format("2006-01-02 15:04:05 UTC")))
	sb.WriteString(fmt.Sprintf(`<div class="meta">Base URL: <strong>%s</strong> | Environment: <strong>%s</strong> | Duration: <strong>%v</strong></div>`, r.BaseURL, r.Environment, r.Duration.Round(time.Millisecond)))

	sb.WriteString(`<div class="summary">`)
	sb.WriteString(fmt.Sprintf(`<div class="card %s"><h3>Total</h3><div class="num">%d</div></div>`, map[bool]string{true: "fail", false: "pass"}[r.Failed > 0], r.Total))
	sb.WriteString(fmt.Sprintf(`<div class="card pass"><h3>Passed</h3><div class="num">%d</div></div>`, r.Passed))
	sb.WriteString(fmt.Sprintf(`<div class="card %s"><h3>Failed</h3><div class="num">%d</div></div>`, map[bool]string{true: "fail", false: "pass"}[r.Failed > 0], r.Failed))
	sb.WriteString(fmt.Sprintf(`<div class="card warn"><h3>Skipped</h3><div class="num">%d</div></div>`, r.Skipped))
	sb.WriteString(`</div>`)

	if r.Diagnostics.FallbackUsed {
		sb.WriteString(`<div class="card warn" style="margin:1rem 0"><h3>⚠️ Fallback Local Usado</h3><p>Produção indisponível — testes executados localmente.</p></div>`)
	}

	sb.WriteString(`<h2>Resultados</h2><table><thead><tr><th>Check</th><th>Categoria</th><th>Status</th><th>Duração</th><th>Mensagem</th><th>Endpoint</th><th>Modo</th></tr></thead><tbody>`)

	// Sort: fails first, then by category
	sort.Slice(r.Results, func(i, j int) bool {
		if r.Results[i].Status != r.Results[j].Status {
			return r.Results[i].Status == "fail"
		}
		return r.Results[i].Category < r.Results[j].Category
	})

	for _, res := range r.Results {
		badge := fmt.Sprintf(`<span class="badge badge-%s">%s</span>`, res.Status, strings.ToUpper(res.Status))
		details := ""
		if len(res.Details) > 0 {
			b, _ := json.MarshalIndent(res.Details, "", "  ")
			details = fmt.Sprintf(`<button onclick="toggleDetails(this)" class="badge badge-skip">Ver detalhes</button><pre class="details" style="display:none">%s</pre>`, string(b))
		}
		sb.WriteString(fmt.Sprintf(`<tr>
			<td><strong>%s</strong></td>
			<td>%s</td>
			<td>%s</td>
			<td>%v</td>
			<td>%s %s</td>
			<td><code>%s %s</code></td>
			<td><span class="badge badge-skip">%s</span></td>
		</tr>`, res.Name, res.Category, badge, res.Duration.Round(time.Millisecond), res.Message, details, res.Method, res.Endpoint, res.RunMode))
	}

	sb.WriteString(`</tbody></table>`)

	if len(r.Diagnostics.FallbackResults) > 0 {
		sb.WriteString(`<h2>Resultados do Fallback Local</h2><table><thead><tr><th>Check</th><th>Status</th><th>Mensagem</th></tr></thead><tbody>`)
		for _, res := range r.Diagnostics.FallbackResults {
			badge := fmt.Sprintf(`<span class="badge badge-%s">%s</span>`, res.Status, strings.ToUpper(res.Status))
			sb.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td></tr>`, res.Name, badge, res.Message))
		}
		sb.WriteString(`</tbody></table>`)
	}

	sb.WriteString(`<script>function toggleDetails(btn){var pre=btn.nextElementSibling;pre.style.display=pre.style.display==='none'?'block':'none';btn.textContent=pre.style.display==='none'?'Ver detalhes':'Ocultar detalhes'}</script></body></html>`)
	return sb.String()
}

func GenerateMarkdown(r *Report) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Health Check Report — %s\n\n", r.Timestamp.Format("2006-01-02 15:04:05 UTC")))
	sb.WriteString(fmt.Sprintf("- **Base URL**: %s  \n", r.BaseURL))
	sb.WriteString(fmt.Sprintf("- **Environment**: %s  \n", r.Environment))
	sb.WriteString(fmt.Sprintf("- **Duration**: %v  \n", r.Duration.Round(time.Millisecond)))
	sb.WriteString(fmt.Sprintf("- **Total**: %d | **Passed**: %d | **Failed**: %d | **Skipped**: %d\n\n", r.Total, r.Passed, r.Failed, r.Skipped))

	if r.Diagnostics.FallbackUsed {
		sb.WriteString("> ⚠️ **Fallback Local Usado** — Produção indisponível, testes executados localmente.\n\n")
	}

	sb.WriteString("## Resultados\n\n")
	sb.WriteString("| Check | Categoria | Status | Duração | Mensagem | Endpoint | Modo |\n")
	sb.WriteString("|-------|-----------|--------|---------|----------|----------|------|\n")

	for _, res := range r.Results {
		statusIcon := map[string]string{"pass": "✅", "fail": "❌", "skip": "⏭️"}[res.Status]
		sb.WriteString(fmt.Sprintf("| %s | %s | %s %s | %v | %s | %s %s | %s |\n",
			res.Name, res.Category, statusIcon, strings.ToUpper(res.Status),
			res.Duration.Round(time.Millisecond), res.Message,
			res.Method, res.Endpoint, res.RunMode))
	}

	return sb.String()
}