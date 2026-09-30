package postgres

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/pedroegerland/wager-service/migrations"
)

type Migrator struct {
	m *migrate.Migrate
}

func NewMigrator(databaseURL string) (*Migrator, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, err
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+stripURLScheme(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Migrator{m: m}, nil
}

func stripURLScheme(u string) string {
	for _, p := range []string{"postgres://", "postgresql://"} {
		if len(u) > len(p) && u[:len(p)] == p {
			return u[len(p):]
		}
	}
	return u
}

func (m *Migrator) Up() error {
	err := m.m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

func (m *Migrator) Down() error {
	err := m.m.Down()
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

func (m *Migrator) Steps(n int) error {
	err := m.m.Steps(n)
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

func (m *Migrator) Version() (uint, bool, error) {
	v, dirty, err := m.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return v, dirty, err
}

func (m *Migrator) Close() error {
	srcErr, dbErr := m.m.Close()
	return errors.Join(srcErr, dbErr)
}
