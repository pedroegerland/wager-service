package money

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const scale = 100

var (
	ErrInvalidAmount      = errors.New("money: invalid amount")
	ErrInvalidCurrency    = errors.New("money: invalid currency")
	ErrCurrencyMismatch   = errors.New("money: currency mismatch")
	ErrOverflow           = errors.New("money: overflow")
	ErrNegativeNotAllowed = errors.New("money: negative amount not allowed")
)

var amountPattern = regexp.MustCompile(`^-?[0-9]+\.[0-9]{2}$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

type Money struct {
	units    int64
	currency string
}

func Parse(amount, currency string) (Money, error) {
	if !currencyPattern.MatchString(currency) {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidCurrency, currency)
	}
	if !amountPattern.MatchString(amount) {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidAmount, amount)
	}
	neg := strings.HasPrefix(amount, "-")
	digits := strings.TrimPrefix(amount, "-")
	digits = strings.Replace(digits, ".", "", 1)
	units, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %q", ErrOverflow, amount)
	}
	if neg {
		units = -units
	}
	return Money{units: units, currency: currency}, nil
}

func ParseNonNegative(amount, currency string) (Money, error) {
	m, err := Parse(amount, currency)
	if err != nil {
		return Money{}, err
	}
	if m.units < 0 {
		return Money{}, fmt.Errorf("%w: %q", ErrNegativeNotAllowed, amount)
	}
	return m, nil
}

func FromUnits(units int64, currency string) (Money, error) {
	if !currencyPattern.MatchString(currency) {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidCurrency, currency)
	}
	return Money{units: units, currency: currency}, nil
}

func MustFromUnits(units int64, currency string) Money {
	m, err := FromUnits(units, currency)
	if err != nil {
		panic(err)
	}
	return m
}

func Zero(currency string) (Money, error) {
	return FromUnits(0, currency)
}

func (m Money) Units() int64     { return m.units }
func (m Money) Currency() string { return m.currency }
func (m Money) IsZero() bool     { return m.units == 0 }
func (m Money) IsPositive() bool { return m.units > 0 }
func (m Money) IsNegative() bool { return m.units < 0 }

func (m Money) IsValid() bool { return m.currency != "" }

func (m Money) requireSameCurrency(o Money) error {
	if !m.IsValid() || !o.IsValid() {
		return ErrInvalidCurrency
	}
	if m.currency != o.currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, o.currency)
	}
	return nil
}

func (m Money) Add(o Money) (Money, error) {
	if err := m.requireSameCurrency(o); err != nil {
		return Money{}, err
	}
	sum := m.units + o.units

	if (m.units > 0 && o.units > 0 && sum < 0) || (m.units < 0 && o.units < 0 && sum >= 0) {
		return Money{}, ErrOverflow
	}
	return Money{units: sum, currency: m.currency}, nil
}

func (m Money) Sub(o Money) (Money, error) {
	if err := m.requireSameCurrency(o); err != nil {
		return Money{}, err
	}
	neg, err := o.Negate()
	if err != nil {
		return Money{}, err
	}
	return m.Add(neg)
}

func (m Money) Negate() (Money, error) {
	if !m.IsValid() {
		return Money{}, ErrInvalidCurrency
	}
	if m.units == math.MinInt64 {
		return Money{}, ErrOverflow
	}
	return Money{units: -m.units, currency: m.currency}, nil
}

func (m Money) Compare(o Money) (int, error) {
	if err := m.requireSameCurrency(o); err != nil {
		return 0, err
	}
	switch {
	case m.units < o.units:
		return -1, nil
	case m.units > o.units:
		return 1, nil
	}
	return 0, nil
}

func (m Money) Equal(o Money) bool {
	return m.currency == o.currency && m.units == o.units
}

func (m Money) String() string {
	u := m.units
	sign := ""
	if u < 0 {
		sign = "-"

		if u == math.MinInt64 {
			return "-92233720368547758.08"
		}
		u = -u
	}
	return fmt.Sprintf("%s%d.%02d", sign, u/scale, u%scale)
}

type DTO struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func (m Money) DTO() DTO {
	return DTO{Amount: m.String(), Currency: m.currency}
}

func (m Money) MarshalJSON() ([]byte, error) {
	if !m.IsValid() {
		return nil, ErrInvalidCurrency
	}
	return []byte(fmt.Sprintf(`{"amount":%q,"currency":%q}`, m.String(), m.currency)), nil
}
