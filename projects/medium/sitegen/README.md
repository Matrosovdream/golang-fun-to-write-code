# sitegen — static site generator with a live-reload dev server

`sitegen build ./site -o ./public` turns Markdown + YAML front matter into a static site (posts,
tag pages, RSS, `sitemap.xml`). `sitegen serve ./site` does the same in memory, watches the
files, rebuilds incrementally and tells the browser to refresh.

## What this project teaches

- **`io/fs` is what makes file code testable.** `Builder.Build(ctx, src fs.FS, out Output)`
  (`internal/site/build.go`) never touches the disk: the CLI passes `os.DirFS` + `DirOutput`,
  the tests pass `fstest.MapFS` + `MemOutput` (`output.go`). The dev server serves straight
  from `MemOutput`, so there is no output directory to go stale.
- **A pipeline with a plan step.** discover → parse → *plan one `unit` per output file* →
  render the stale ones on a worker pool → write → prune. `siteData.units` is the plan.
- **Content-hash incremental builds.** A unit's key is the sha256 of the config, the whole
  template set, and `URL + source hash` of every page it shows (`siteData.key`). `Builder.execute`
  skips units whose key matches the cache. Edit one post and exactly that post, the index, the
  tag pages that list it, the feed and the sitemap re-render (`TestIncrementalBuild` asserts the
  exact set of files written). A failed unit keeps its old key and is retried.
- **Worker pool without a mutex.** Workers write to distinct `errs[i]` slots; the cache is only
  touched after `wg.Wait()`. `TestWorkerCountDoesNotChangeOutput` runs 1 and 8 workers under `-race`.
- **Front matter by hand.** `splitFrontMatter` (`page.go`) finds the `---` fences itself
  (CRLF, trailing spaces, a `---` rule in the body, a missing closing fence) and hands the YAML to
  `yaml.v3` with `KnownFields(true)`, so a typo like `tag:` is an error. Every error carries the file name.
- **`html/template` done properly.** One parsed set per layout, because a namespace holds one
  `{{define "content"}}` (`loadTemplates`, `templates.go`). Layouts only `define` the blocks
  `base.html` declares with `block`; partials (`postlist.html`) are shared; a file with the same
  name in the site's `templates/` replaces the embedded theme's. `TestHTMLIsEscaped` puts
  `<script>` in a title; goldmark drops raw HTML in Markdown, which is what makes
  `template.HTML(body)` safe.
- **`go:embed` for the default theme and the reload script** (`theme/`, `serve/reload.js`).
- **RSS and sitemap with `encoding/xml`** (`feed.go`): `xml:"channel>title"` nests a field
  inside `<channel>` without a wrapper struct. The tests unmarshal the feed with *separate* structs
  and check item order, dates and that `Tom & <Jerry>` round-trips.
- **Why a file watcher fires five events per save.** `watch.Dir` (`watch/watch.go`) forwards
  events; `Ignored` drops dotfiles, `*~`, `*.swp`, `.#*`. fsnotify is not recursive, so new
  directories are added as they appear, and the watch is added *before* the event is forwarded.
- **A debouncer you can test.** `watch.Debounce` (`debounce.go`) restarts a timer on every event
  and signals once it is quiet; a `nil` channel disables the timer case while nothing is pending;
  the output holds one pending signal so a busy rebuild never blocks the watcher. Time is a
  `Clock` interface, and the tests fire timers by hand.
- **Server-Sent Events.** `Hub.ServeHTTP` (`serve/hub.go`): `http.Flusher`, `r.Context().Done()`
  to stop, a non-blocking fan-out so a stalled tab cannot stall a build, a build error replayed to
  tabs that connect afterwards. `Server.Run` derives request contexts from its own context via
  `BaseContext`, because `Shutdown` does not cancel them and an SSE response never ends by itself.
- **Inject at response time, never at build time.** `inject` (`serve/serve.go`) puts the script
  before `</body>` on the in-memory copy; `TestBuildTheExampleSite` greps every file `build` wrote
  to prove the script is not there. A broken rebuild keeps serving the last good site and shows the
  error in the browser and the terminal.

## Run

```sh
go test -race ./...

go run ./cmd/sitegen build example -o public
```

```
built example -> public: 15 files written
```

```sh
find public -type f | sort
```

```
public/about/index.html
public/feed.xml
public/index.html
public/posts/embedding-a-theme/index.html
public/posts/hello-sitegen/index.html
public/posts/live-reload-with-sse/index.html
public/posts/why-debounce/index.html
public/robots.txt
public/sitemap.xml
public/style.css
public/tags/embed/index.html
public/tags/go/index.html
public/tags/meta/index.html
public/tags/tooling/index.html
public/tags/web/index.html
```

(`example/` has a fifth post marked `draft: true`; `-drafts` adds it, 16 files.)

The dev server, an editor-style save, and what a browser's SSE connection receives:

```sh
work=$(mktemp -d)
go build -o "$work/sitegen" ./cmd/sitegen
cp -R example "$work/site"

"$work/sitegen" serve -addr 127.0.0.1:8103 "$work/site" & server=$!
sleep 1
curl -s localhost:8103/ | grep -A1 EventSource      # the injected script
curl -sN --max-time 4 localhost:8103/__reload &     # what a browser keeps open
sleep 1
echo 'Edited.' >> "$work/site/content/posts/hello-sitegen.md"   # one editor "save"
sleep 4
kill $server; wait $server
rm -rf "$work"
```

```
built in 3ms: 15 rendered, 0 unchanged, 0 removed
serving $work/site on http://127.0.0.1:8103 (ctrl+c to stop)
const es = new EventSource("/__reload");
es.addEventListener("reload", () => location.reload());
: connected

built in 2ms: 6 rendered, 9 unchanged, 0 removed
event: reload
data: 

```

Six files re-rendered for one edited post: the post, the index, its two tag pages, the feed and
the sitemap. Open `http://127.0.0.1:8103` in a browser instead of curl to see the page refresh;
break a post's front matter and a red banner with the error appears while the old page keeps
being served.

## Rewrite exercises

1. Rewrite `Builder.execute` and `siteData.key` from memory: stale detection, the worker pool,
   cache update only on success, pruning. Then break the key (leave the templates out of it)
   and watch `TestIncrementalBuild` catch it.
2. Persist the cache as `.sitegen-cache.json` inside the output directory so a second
   `sitegen build` *process* skips unchanged pages. What must happen when the output directory is
   deleted but the cache file is not?
3. Add previous/next links to posts. Whose key now has to include whom? Write the failing
   incremental test first.
4. Replace `Debounce` with a `time.AfterFunc` + mutex version. Make a rebuild that takes longer
   than the debounce window overlap with the next one, and see what the channel version gave you for free.
5. Add a `/tags/` index page, then write a benchmark that generates 2,000 posts and compares a
   full build against a one-post-changed rebuild. Look at where the time goes with `-cpuprofile`.

## Deliberately left out

- **Size.** The Go source is about 900 lines (excluding blanks and comments), over the usual
  medium target. It is three subsystems the spec asks for: the build pipeline with templates,
  feeds and outputs (~540 lines), the watcher and debouncer (~100), the dev server with SSE (~175),
  plus the CLI (~85). The embedded theme, the example site and the reload script are not Go.
- The incremental cache lives in the `Builder`, so it works across rebuilds inside one process
  (`serve`), not across separate `sitegen build` runs (exercise 2).
- `build` never deletes files that an earlier run left in `-o`; only an in-process rebuild prunes.
- Links are root-relative: no hosting under a sub-path. Two sources that slug to the same URL
  (`Hello World.md`, `hello-world.md`) overwrite each other silently.
- No pagination, no tags index page, no syntax highlighting, no `static/` fingerprinting.
- Watcher errors (for example an inotify queue overflow) are drained, not reported.
