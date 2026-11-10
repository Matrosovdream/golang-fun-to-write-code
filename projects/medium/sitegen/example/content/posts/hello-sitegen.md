---
title: Hello, sitegen
date: 2026-03-02
tags: [go, meta]
summary: The first post, written to see the whole pipeline end to end.
---
Every page here started life as a Markdown file with a **YAML front matter** block.

| Stage    | Package        |
| -------- | -------------- |
| discover | `io/fs`        |
| parse    | `yaml.v3`      |
| render   | `html/template` |

~~Static sites are boring.~~ Static sites are fast.
