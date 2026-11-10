---
title: Embedding a theme
date: 2026-03-09
tags: [go, embed]
summary: Ship templates inside the binary and still let users override them.
---
The default theme lives in the binary through `go:embed`:

```go
//go:embed theme
var themeFS embed.FS
```

A file with the same name in the site's own `templates/` directory wins.
This site overrides `base.html` to add an About link to the header.
