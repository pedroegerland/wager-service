package money

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in    string
		cur   string
		units int64
		err   error
	}{
		{"25.00", "BRL", 2500, nil},
		{"0.00", "BRL", 0, nil},
		{"0.01", "BRL", 1, nil},
		{"-3.50", "BRL", -350, nil},
		{"92233720368547758.07", "BRL", math.MaxInt64, nil},
		{"92233720368547758.08", "BRL", 0, ErrOverflow},
		{"25", "BRL", 0, ErrInvalidAmount},
		{"25.0", "BRL", 0, ErrInvalidAmount},
		{"25.000", "BRL", 0, ErrInvalidAmount},
		{"2.5e1", "BRL", 0, ErrInvalidAmount},
		{"", "BRL", 0, ErrInvalidAmount},
		{"NaN", "BRL", 0, ErrInvalidAmount},
		{"Infinity", "BRL", 0, ErrInvalidAmount},
		{" 25.00", "BRL", 0, ErrInvalidAmount},
		{"25.00", "brl", 0, ErrInvalidCurrency},
		{"25.00", "", 0, ErrInvalidCurrency},
		{"25.00", "BRLX", 0, ErrInvalidCurrency},
	}
	for _, c := range cases {
		m, err := Parse(c.in, c.cur)
		if !errors.Is(err, c.err) {
			t.Errorf("Parse(%q,%q) err=%v want %v", c.in, c.cur, err, c.err)
			continue
		}
		if err == nil && m.Units() != c.units {
			t.Errorf("Parse(%q) units=%d want %d", c.in, m.Units(), c.units)
		}
	}
}

func TestParseNonNegative(t *testing.T) {
	if _, err := ParseNonNegative("-1.00", "BRL"); !errors.Is(err, ErrNegativeNotAllowed) {
		t.Fatalf("got %v", err)
	}
	if _, err := ParseNonNegative("0.00", "BRL"); err != nil {
		t.Fatalf("zero should be accepted: %v", err)
	}
}

func TestString(t *testing.T) {
	cases := map[int64]string{
		0:             "0.00",
		1:             "0.01",
		2500:          "25.00",
		-350:          "-3.50",
		math.MaxInt64: "92233720368547758.07",
		math.MinInt64: "-92233720368547758.08",
	}
	for u, want := range cases {
		if got := MustFromUnits(u, "BRL").String(); got != want {
			t.Errorf("%d -> %q want %q", u, got, want)
		}
	}
}

func TestArithmetic(t *testing.T) {
	a := MustFromUnits(1000, "BRL")
	b := MustFromUnits(250, "BRL")

	sum, err := a.Add(b)
	if err != nil || sum.Units() != 1250 {
		t.Fatalf("add: %v %v", sum, err)
	}
	diff, err := a.Sub(b)
	if err != nil || diff.Units() != 750 {
		t.Fatalf("sub: %v %v", diff, err)
	}
	neg, err := b.Sub(a)
	if err != nil || neg.Units() != -750 {
		t.Fatalf("negative diff should be fine internally: %v %v", neg, err)
	}
	n, _ := b.Negate()
	if n.Units() != -250 {
		t.Fatalf("negate: %v", n)
	}
	cmp, _ := a.Compare(b)
	if cmp != 1 {
		t.Fatalf("compare: %d", cmp)
	}
}

func TestCurrencyMismatch(t *testing.T) {
	brl := MustFromUnits(100, "BRL")
	usd := MustFromUnits(100, "USD")
	if _, err := brl.Add(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("add: %v", err)
	}
	if _, err := brl.Sub(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("sub: %v", err)
	}
	if _, err := brl.Compare(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("compare: %v", err)
	}
	if brl.Equal(usd) {
		t.Error("equal should be false across currencies")
	}
}

func TestOverflow(t *testing.T) {
	max := MustFromUnits(math.MaxInt64, "BRL")
	one := MustFromUnits(1, "BRL")
	if _, err := max.Add(one); !errors.Is(err, ErrOverflow) {
		t.Errorf("add overflow: %v", err)
	}
	min := MustFromUnits(math.MinInt64, "BRL")
	if _, err := min.Sub(one); !errors.Is(err, ErrOverflow) {
		t.Errorf("sub overflow: %v", err)
	}
	if _, err := min.Negate(); !errors.Is(err, ErrOverflow) {
		t.Errorf("negate overflow: %v", err)
	}
}

func TestZeroValueIsRejected(t *testing.T) {
	var z Money
	if z.IsValid() {
		t.Fatal("zero struct must not be valid")
	}
	if _, err := z.Add(MustFromUnits(1, "BRL")); err == nil {
		t.Fatal("expected error on uninitialized money")
	}
	if _, err := json.Marshal(z); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestJSON(t *testing.T) {
	b, err := json.Marshal(MustFromUnits(2500, "BRL"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"amount":"25.00","currency":"BRL"}` {
		t.Fatalf("got %s", b)
	}
}
