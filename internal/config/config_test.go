package config

import (
	"strings"
	"testing"
)

func valid() Config {
	return Config{RateLimitRPS: 10, ReferenceMaxAttempts: 3, SQSVisibilityTimeout: 30, DBMaxConns: 4}
}

func TestValidate(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Config){
		"RATE_LIMIT_RPS":         func(c *Config) { c.RateLimitRPS = 0 },
		"REFERENCE_MAX_ATTEMPTS": func(c *Config) { c.ReferenceMaxAttempts = 0 },
		"SQS_VISIBILITY_TIMEOUT": func(c *Config) { c.SQSVisibilityTimeout = 1 },
		"DB_MAX_CONNS":           func(c *Config) { c.DBMaxConns = 1 },
	}
	for name, mutate := range cases {
		c := valid()
		mutate(&c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLoadRequiresDatabaseAndAuth(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error without DATABASE_URL")
	}
	for k, v := range map[string]string{
		"DATABASE_URL": "postgres://x", "OIDC_ISSUER": "i", "OIDC_JWKS_URL": "j",
		"SQS_INBOUND_QUEUE_URL": "a", "SQS_DLQ_URL": "b", "SQS_EVENTS_QUEUE_URL": "c",
	} {
		t.Setenv(k, v)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.OIDCAudience != "wager-api" || cfg.RateLimitRPS != 50 {
		t.Errorf("defaults: %+v", cfg)
	}
}
