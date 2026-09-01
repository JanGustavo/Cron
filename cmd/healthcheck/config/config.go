package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	BaseURL         string
	LocalBaseURL    string
	TestEmail       string
	TestPassword    string
	TestCPF         string
	PreAuthToken    string
	JWTSecret       string
	AllowFailures   bool
	RunOnce         bool
	Server          ServerConfig
	Schedule        ScheduleConfig
	Notifier        NotifierConfig
	Report          ReportConfig
}

type ServerConfig struct {
	Enabled bool
	Port    int
}

type ScheduleConfig struct {
	Enabled  bool
	Interval time.Duration
}

type NotifierConfig struct {
	Email    EmailConfig
	Slack    SlackConfig
	Webhook  WebhookConfig
	Disabled bool
}

type EmailConfig struct {
	Enabled  bool
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	From     string
	To       []string
}

type SlackConfig struct {
	Enabled    bool
	WebhookURL string
	Channel    string
}

type WebhookConfig struct {
	Enabled bool
	URL     string
}

type ReportConfig struct {
	Dir         string
	Retention   time.Duration
	JSONEnabled bool
	HTMLEnabled bool
}

func Load() *Config {
	return &Config{
		BaseURL:        getEnv("HEALTHCHECK_BASE_URL", "https://cronflow.jangustavo.me"),
		LocalBaseURL:   getEnv("HEALTHCHECK_LOCAL_URL", "http://localhost:8080"),
		TestEmail:      getEnv("HEALTHCHECK_TEST_EMAIL", "healthcheck_"+randomSuffix()+"@test.cronflow.sh"),
		TestPassword:   getEnv("HEALTHCHECK_TEST_PASSWORD", "HealthCheck123!"),
		TestCPF:        getEnv("HEALTHCHECK_TEST_CPF", "12345678909"),
		PreAuthToken:   getEnv("HEALTHCHECK_PRE_AUTH_TOKEN", ""),
		JWTSecret:      getEnv("JWT_SECRET", ""),
		AllowFailures:  getBoolEnv("HEALTHCHECK_ALLOW_FAILURES", false),
		RunOnce:        getBoolEnv("HEALTHCHECK_RUN_ONCE", false),
		Server: ServerConfig{
			Enabled: getBoolEnv("HEALTHCHECK_SERVER_ENABLED", true),
			Port:    getIntEnv("HEALTHCHECK_SERVER_PORT", 8081),
		},
		Schedule: ScheduleConfig{
			Enabled:  getBoolEnv("HEALTHCHECK_SCHEDULE_ENABLED", true),
			Interval: getDurationEnv("HEALTHCHECK_SCHEDULE_INTERVAL", 15*time.Minute),
		},
		Notifier: NotifierConfig{
			Disabled: getBoolEnv("HEALTHCHECK_NOTIFIER_DISABLED", false),
			Email: EmailConfig{
				Enabled:  getBoolEnv("HEALTHCHECK_EMAIL_ENABLED", false),
				SMTPHost: getEnv("SMTP_HOST", "smtp.gmail.com"),
				SMTPPort: getIntEnv("SMTP_PORT", 587),
				Username: getEnv("SMTP_USER", ""),
				Password: getEnv("SMTP_PASS", ""),
				From:     getEnv("SMTP_FROM", "healthcheck@cronflow.sh"),
				To:       getStringSliceEnv("HEALTHCHECK_EMAIL_TO", []string{}),
			},
			Slack: SlackConfig{
				Enabled:    getBoolEnv("HEALTHCHECK_SLACK_ENABLED", false),
				WebhookURL: getEnv("HEALTHCHECK_SLACK_WEBHOOK", ""),
				Channel:    getEnv("HEALTHCHECK_SLACK_CHANNEL", "#alerts"),
			},
			Webhook: WebhookConfig{
				Enabled: getBoolEnv("HEALTHCHECK_WEBHOOK_ENABLED", false),
				URL:     getEnv("HEALTHCHECK_WEBHOOK_URL", ""),
			},
		},
		Report: ReportConfig{
			Dir:         getEnv("HEALTHCHECK_REPORT_DIR", "/tmp/healthcheck-reports"),
			Retention:   getDurationEnv("HEALTHCHECK_REPORT_RETENTION", 7*24*time.Hour),
			JSONEnabled: getBoolEnv("HEALTHCHECK_REPORT_JSON", true),
			HTMLEnabled: getBoolEnv("HEALTHCHECK_REPORT_HTML", true),
		},
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getStringSliceEnv(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		return []string{v}
	}
	return fallback
}

func randomSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}