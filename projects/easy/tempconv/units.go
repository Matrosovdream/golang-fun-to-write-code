package main

import (
	"errors"
	"math"
	"strconv"
)

// Named types give plain float64s a unit identity: the compiler now refuses
// Celsius + Fahrenheit, and each type carries its own methods.
type (
	Celsius    float64
	Fahrenheit float64
	Kelvin     float64

	Meters float64
	Feet   float64

	Kilograms float64
	Pounds    float64
)

const AbsoluteZeroC Celsius = -273.15

var ErrBelowAbsoluteZero = errors.New("temperature below absolute zero")

func (c Celsius) ToF() Fahrenheit { return Fahrenheit(c*9/5 + 32) }
func (c Celsius) ToK() Kelvin     { return Kelvin(c - AbsoluteZeroC) }
func (f Fahrenheit) ToC() Celsius { return Celsius((f - 32) * 5 / 9) }
func (k Kelvin) ToC() Celsius     { return Celsius(k) + AbsoluteZeroC }

func (m Meters) ToFeet() Feet   { return Feet(m * 3.28084) }
func (f Feet) ToMeters() Meters { return Meters(f / 3.28084) }

func (kg Kilograms) ToLb() Pounds { return Pounds(kg * 2.20462) }
func (lb Pounds) ToKg() Kilograms { return Kilograms(lb / 2.20462) }

// String implements fmt.Stringer, so %v (and fmt.Sprint, log, ...) print
// units automatically — no formatting code at call sites, ever.
func (c Celsius) String() string    { return trim(float64(c)) + "°C" }
func (f Fahrenheit) String() string { return trim(float64(f)) + "°F" }
func (k Kelvin) String() string     { return trim(float64(k)) + "K" }
func (m Meters) String() string     { return trim(float64(m)) + "m" }
func (f Feet) String() string       { return trim(float64(f)) + "ft" }
func (kg Kilograms) String() string { return trim(float64(kg)) + "kg" }
func (lb Pounds) String() string    { return trim(float64(lb)) + "lb" }

// trim rounds to 2 decimals and drops trailing zeros: 212 not 212.00,
// but 98.6 stays 98.6.
func trim(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}
