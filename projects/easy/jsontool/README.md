# E08 jsontool — JSON pretty-printer & path extractor

`cat api.json | jsontool` pretty-prints; `jsontool -path users.0.name api.json` digs values out.

## What it teaches

- **Dynamic JSON** (`any` / `map[string]any` / `[]any`) when you don't own the schema —
  and the **type switch + type assertion** dance that navigates it.
- **`json.Number` via `UseNumber()`**: default decoding turns every number into `float64`,
  silently corrupting big int64 IDs — `TestLargeIntSurvives` proves it.
- Error messages that name *where* in the path things went wrong.
- `json.MarshalIndent`, decoder-from-reader (streams, not `ReadAll` + `Unmarshal`).
- Unix-tool ergonomics: stdin by default, bare strings printed unquoted.

## Run

```sh
go test ./...
echo '{"users":[{"name":"ada"},{"name":"linus"}]}' | go run . -path users.1.name
curl -s https://api.github.com/repos/golang/go | go run . -path owner.login
```

## Rewrite exercises

1. Rebuild `extract` from its doc comment; port the error-message tests.
2. Add wildcard support: `users.*.name` returns a JSON array of all names.
3. Add `-keys` to list an object's keys — sorted, of course.
