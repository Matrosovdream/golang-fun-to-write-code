// tempconv converts between units:
//
//	tempconv 100C 72F 300K 10m 33ft 70kg 154lb
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tempconv <value><unit>...   units: C F K m ft kg lb")
		os.Exit(2)
	}
	for _, arg := range os.Args[1:] {
		line, err := convert(arg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tempconv:", err)
			os.Exit(1)
		}
		fmt.Println(line)
	}
}

func convert(arg string) (string, error) {
	num, unit := splitUnit(arg)
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return "", fmt.Errorf("%q: not a number followed by a unit", arg)
	}

	switch strings.ToLower(unit) {
	case "c":
		c := Celsius(v)
		if c < AbsoluteZeroC {
			return "", fmt.Errorf("%v: %w", c, ErrBelowAbsoluteZero)
		}
		return fmt.Sprintf("%v = %v = %v", c, c.ToF(), c.ToK()), nil
	case "f":
		f := Fahrenheit(v)
		if f.ToC() < AbsoluteZeroC {
			return "", fmt.Errorf("%v: %w", f, ErrBelowAbsoluteZero)
		}
		return fmt.Sprintf("%v = %v = %v", f, f.ToC(), f.ToC().ToK()), nil
	case "k":
		if v < 0 {
			return "", fmt.Errorf("%vK: %w", v, ErrBelowAbsoluteZero)
		}
		k := Kelvin(v)
		return fmt.Sprintf("%v = %v = %v", k, k.ToC(), k.ToC().ToF()), nil
	case "m":
		m := Meters(v)
		return fmt.Sprintf("%v = %v", m, m.ToFeet()), nil
	case "ft":
		f := Feet(v)
		return fmt.Sprintf("%v = %v", f, f.ToMeters()), nil
	case "kg":
		kg := Kilograms(v)
		return fmt.Sprintf("%v = %v", kg, kg.ToLb()), nil
	case "lb":
		lb := Pounds(v)
		return fmt.Sprintf("%v = %v", lb, lb.ToKg()), nil
	default:
		return "", fmt.Errorf("%q: unknown unit %q (want C, F, K, m, ft, kg or lb)", arg, unit)
	}
}

// splitUnit separates "100.5C" into "100.5" and "C": the unit is the
// trailing run of letters.
func splitUnit(arg string) (num, unit string) {
	i := len(arg)
	for i > 0 && unicode.IsLetter(rune(arg[i-1])) {
		i--
	}
	return arg[:i], arg[i:]
}
