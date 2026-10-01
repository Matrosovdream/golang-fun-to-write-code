package main

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestConversions(t *testing.T) {
	tests := []struct {
		arg  string
		want string
	}{
		{"100C", "100°C = 212°F = 373.15K"},
		{"98.6F", "98.6°F = 37°C = 310.15K"},
		{"0K", "0K = -273.15°C = -459.67°F"},
		{"-40c", "-40°C = -40°F = 233.15K"}, // the famous crossing point; units are case-insensitive
		{"100m", "100m = 328.08ft"},
		{"1kg", "1kg = 2.2lb"},
	}
	for _, tc := range tests {
		t.Run(tc.arg, func(t *testing.T) {
			got, err := convert(tc.arg)
			if err != nil {
				t.Fatalf("convert(%q): %v", tc.arg, err)
			}
			if got != tc.want {
				t.Errorf("convert(%q) = %q, want %q", tc.arg, got, tc.want)
			}
		})
	}
}

func TestBelowAbsoluteZero(t *testing.T) {
	for _, arg := range []string{"-300C", "-500F", "-1K"} {
		_, err := convert(arg)
		// errors.Is survives the %w wrapping in convert.
		if !errors.Is(err, ErrBelowAbsoluteZero) {
			t.Errorf("convert(%q): err = %v, want ErrBelowAbsoluteZero", arg, err)
		}
	}
}

func TestBadInput(t *testing.T) {
	for _, arg := range []string{"", "abc", "12", "12parsec", "C"} {
		if _, err := convert(arg); err == nil {
			t.Errorf("convert(%q): expected an error", arg)
		}
	}
}

func TestStringerIsPickedUpByFmt(t *testing.T) {
	// %v finds the String method on its own — that's the whole point.
	if got := fmt.Sprintf("%v", Celsius(21.5)); got != "21.5°C" {
		t.Errorf("got %q, want %q", got, "21.5°C")
	}
}

func TestRoundTrip(t *testing.T) {
	for _, c := range []Celsius{-40, 0, 36.6, 100} {
		back := c.ToF().ToC()
		if math.Abs(float64(back-c)) > 1e-9 {
			t.Errorf("C→F→C drifted: %v → %v", c, back)
		}
	}
}
