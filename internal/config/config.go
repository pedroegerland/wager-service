package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	HTTPAddr        string        `env:"HTTP_ADDR" envDefault:":8080"`
	LogLevel        string        `env:"LOG_LEVEL" envDefault:"info"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"25s"`

	RateLimitRPS   float64 `env:"RATE_LIMIT_RPS" envDefault:"50"`
	RateLimitBurst int     `env:"RATE_LIMIT_BURST" envDefault:"100"`

	DatabaseURL        string        `env:"DATABASE_URL,required"`
	DBMaxConns         int32         `env:"DB_MAX_CONNS" envDefault:"20"`
	DBStatementTimeout time.Duration `env:"DB_STATEMENT_TIMEOUT" envDefault:"10s"`
	MigrateOnStart     bool          `env:"MIGRATE_ON_START" envDefault:"false"`

	OIDCIssuer   string `env:"OIDC_ISSUER,required"`
	OIDCJWKSURL  string `env:"OIDC_JWKS_URL,required"`
	OIDCAudience string `env:"OIDC_AUDIENCE" envDefault:"wager-api"`

	AWSRegion          string `env:"AWS_REGION" envDefault:"us-east-1"`
	AWSEndpoint        string `env:"AWS_ENDPOINT_URL"`
	AWSAccessKeyID     string `env:"AWS_ACCESS_KEY_ID"`
	AWSSecretAccessKey string `env:"AWS_SECRET_ACCESS_KEY"`

	SQSInboundQueueURL   string `env:"SQS_INBOUND_QUEUE_URL,required"`
	SQSDLQURL            string `env:"SQS_DLQ_URL,required"`
	SQSEventsQueueURL    string `env:"SQS_EVENTS_QUEUE_URL,required"`
	SQSWorkers           int    `env:"SQS_WORKERS" envDefault:"2"`
	SQSVisibilityTimeout int32  `env:"SQS_VISIBILITY_TIMEOUT" envDefault:"30"`
	SQSMaxReceiveCount   int    `env:"SQS_MAX_RECEIVE_COUNT" envDefault:"5"`
	ConsumerEnabled      bool   `env:"CONSUMER_ENABLED" envDefault:"true"`

	OutboxEnabled      bool          `env:"OUTBOX_ENABLED" envDefault:"true"`
	OutboxPollInterval time.Duration `env:"OUTBOX_POLL_INTERVAL" envDefault:"500ms"`
	OutboxBatchSize    int           `env:"OUTBOX_BATCH_SIZE" envDefault:"50"`
	OutboxLease        time.Duration `env:"OUTBOX_LEASE" envDefault:"30s"`

	ReferenceMaxAttempts  int           `env:"REFERENCE_MAX_ATTEMPTS" envDefault:"6"`
	ReferenceBaseBackoff  time.Duration `env:"REFERENCE_BASE_BACKOFF" envDefault:"2s"`
	ReferenceMaxBackoff   time.Duration `env:"REFERENCE_MAX_BACKOFF" envDefault:"2m"`
	ReferencePollInterval time.Duration `env:"REFERENCE_POLL_INTERVAL" envDefault:"1s"`
}

func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	var errs []error
	if c.RateLimitRPS <= 0 {
		errs = append(errs, errors.New("RATE_LIMIT_RPS must be > 0"))
	}
	if c.ReferenceMaxAttempts < 1 {
		errs = append(errs, errors.New("REFERENCE_MAX_ATTEMPTS must be >= 1"))
	}
	if c.SQSVisibilityTimeout < 5 {
		errs = append(errs, errors.New("SQS_VISIBILITY_TIMEOUT must be >= 5"))
	}
	if c.DBMaxConns < 2 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be >= 2"))
	}
	return errors.Join(errs...)
}
