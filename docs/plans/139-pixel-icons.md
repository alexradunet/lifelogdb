# 139 — Pixel icons: the icons of the rail, the New menu and the kinds as 10 by 10 glyphs

> Dated implementation record. Tests use temporary folders and synthetic data. The owner's live `life.db` is never
> opened by a test.

## Status

- **Date / baseline:** 2026-10-19, `5dce8b1` (master).
- **Priority:** P3. **Effort:** S. **Risk:** LOW: one drawing, one template function, the sheets.
- **Status:** TODO.
- **Resolves:** the last candidate of part 6 of [plan 137](137-pixel-wood-and-the-focus-pass.md). The page of
  mock-ups left the icons out by the agent's call; the owner's word was "everything except the leaf on rings", and
  then "Where are the icons?".

## The decision

| question | decision |
|---|---|
| the icons | every icon the browser draws (`icon` in `shell.go`: today, calendar, browse, people, places, files, metrics, search, new, more, image, video, audio) becomes a pixel glyph of 10 by 10 pixels, drawn at two screen pixels a pixel (20 by 20) where the stroked icon was 16 by 16, and at four where a tile draws it large |
| how they ship | one more sprite sheet, `html/icons.png`, drawn by `internal/pixelart` from rows of characters, one cell of 16 by 16 a glyph, in the order of the names above; the `icon` function renders `<span class="icon icon-NAME" aria-hidden="true"></span>` in place of the inline SVG, and the sheet draws the cell by position |
| the colour | the glyph is `currentColor` of the stroked icon today; a sprite has one colour, so the sheet is drawn in the rail's ink and the glyph is recoloured with `mask-image` over `background-color:currentColor`, which keeps every place where an icon follows its text (the rail's ink, the gold of the current section, the muted ink of a tile) |

## The drawings

The rows of the session's mock-up (2026-10-19), one character a pixel, `X` drawn: today a sun of eight rays,
calendar a grid with a header row, metrics a rising line with two breaks, browse a folder with a tab, search a lens
with a handle, new a cross of two pixels, more three pairs, people a head over shoulders, places a drop with a
hollow, files a page with a folded corner. The three kinds of the media list (image, video, audio) are drawn in the
same hand: a frame with a hill and a sun, a frame with a triangle, a microphone on a stand.

## What changes where

| path | change |
|---|---|
| `internal/pixelart/icons.go`, `icons_test.go` | `Icons()`: the sheet of 13 cells; the names in order, exported as one list the view reads; determinism, size, one known pixel a glyph |
| `tools/pixelart/main.go` | writes `icons.png` too |
| `internal/api/view/html/icons.png` | the file |
| `internal/api/view/shell.go`, `shell_test.go` | `icon` renders the span; the map of SVG paths goes; a test that every name the templates use is a cell of the sheet (read the names from `pixelart`) |
| `internal/api/serve.go`, `public.go`, `assets_test.go` | the route, an install file |
| `internal/api/view/sheets_test.go` | the comparison of the fourth picture; the positions in the sheet built from the constants, as for the sprites |
| `internal/api/view/html/app.css` | `span.icon`: 20 by 20, `mask-image:url(/assets/icons.png)` with `mask-size` and `mask-position` by cell, `background-color:currentColor`, pixelated; the phone's bottom bar and the media tiles set their sizes; `svg.icon` rules go |
| `README.md` | "three embedded pictures" becomes four |

## Checks

- `gofmt -l`, `go vet ./... && go test ./...`; `staticcheck ./...`, `govulncheck ./...`.
- Screenshots on the review server: the rail, the bottom bar, the New menu, the More sheet, Browse's media tiles, a
  search with hits and their kind icons; desktop and phone; light and dark.
- A counter-review with the real binary; the owner's deploy.

## Done when

- No inline SVG icon remains; every icon is a cell of `icons.png` drawn in the colour of its text.
- The rail, the bars, the menus and the tiles show the glyphs crisp at 20px and 40px in both schemes.
