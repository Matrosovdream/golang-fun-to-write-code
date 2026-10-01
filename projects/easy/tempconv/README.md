# E02 tempconv — unit converter

`tempconv 100C 98.6F 10m 70kg` → each argument printed in its sibling units.

## What it teaches

- **Named types** (`type Celsius float64`): the compiler stops you mixing units, and plain
  numbers become method receivers.
- Methods with value receivers; when a pointer receiver would be pointless.
- **`fmt.Stringer`**: implement `String()` once, and `%v` prints `21.5°C` everywhere forever.
- Typed constants (`AbsoluteZeroC`), sentinel error + `%w` wrapping + `errors.Is`.
- `strconv.ParseFloat` / `FormatFloat` (the `-1` precision trick), float rounding for display.
- Parsing by hand (`splitUnit`) — no regexp needed for simple shapes.
- Table-driven tests including a round-trip property test.

## Run

```sh
go test ./...
go run . 100C 98.6F 0K -40C 10m 33ft 70kg 154lb
go run . -300C   # error path: below absolute zero
```

## Rewrite exercises

1. Rebuild from the type list; write the tests first this time.
2. Add liters/gallons without touching existing code — notice what the design made easy.
3. Make `-40c` work but `-40X` fail with a helpful message listing valid units (it does — keep it that way in your version).
