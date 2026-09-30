package postgres

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pedroegerland/wager-service/internal/app/port"
)

func TestLedgerCursorRoundTrip(t *testing.T) {
	for _, seq := range []int64{0, 1, 42, 1 << 40} {
		got, err := decodeCursor(encodeCursor(seq))
		if err != nil || got != seq {
			t.Errorf("%d: %d %v", seq, got, err)
		}
	}
	if got, err := decodeCursor(""); err != nil || got != 0 {
		t.Errorf("empty cursor: %d %v", got, err)
	}
	for _, bad := range []string{"abc*", "LTE", "bm90YW51bWJlcg"} {
		if _, err := decodeCursor(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestTranslateError(t *testing.T) {
	cases := []struct {
		in   error
		want error
	}{
		{nil, nil},
		{pgx.ErrNoRows, port.ErrNotFound},
		{&pgconn.PgError{Code: "23505", ConstraintName: "wallets_player_currency_unique"}, port.ErrConflict},
		{&pgconn.PgError{Code: "40001"}, port.ErrUnavailable},
		{&pgconn.PgError{Code: "40P01"}, port.ErrUnavailable},
		{&pgconn.PgError{Code: "0A000", Message: "cached plan must not change result type"}, port.ErrUnavailable},
		{&pgconn.PgError{Code: "57P01"}, port.ErrUnavailable},
		{context.DeadlineExceeded, port.ErrUnavailable},
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, port.ErrUnavailable},
	}
	for _, c := range cases {
		got := translateError(c.in)
		if c.want == nil {
			if got != nil {
				t.Errorf("%v: expected nil, got %v", c.in, got)
			}
			continue
		}
		if !errors.Is(got, c.want) {
			t.Errorf("%v: got %v want %v", c.in, got, c.want)
		}
	}
	var ce *port.ConflictError
	if err := translateError(&pgconn.PgError{Code: "23505", ConstraintName: "x"}); !errors.As(err, &ce) || ce.Constraint != "x" {
		t.Errorf("conflict should carry the constraint name: %v", err)
	}
	other := &pgconn.PgError{Code: "22P02"}
	if err := translateError(other); !errors.Is(err, other) {
		t.Errorf("unknown codes pass through: %v", err)
	}
}

func TestStripURLScheme(t *testing.T) {
	cases := map[string]string{
		"postgres://u:p@h/db":   "u:p@h/db",
		"postgresql://u:p@h/db": "u:p@h/db",
		"u:p@h/db":              "u:p@h/db",
	}
	for in, want := range cases {
		if got := stripURLScheme(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
