# site/ — the project's website

One static page: `index.html` and `style.css`, with no script and no build step. The workflow
[`.github/workflows/pages.yml`](../.github/workflows/pages.yml) publishes this folder to GitHub Pages on each push to
`master` that changes it.

- To see it, open `index.html` in a browser. Every path is relative.
- The look is the application's: the tokens in `style.css` are a subset of the token block in
  [`internal/api/html/base.css`](../internal/api/html/base.css), with the same values. When a token changes there,
  change it here too.
- `fonts/` and `icon.svg` are copies of the application's own files in
  [`internal/api/html/`](../internal/api/html/). The fonts are under the SIL Open Font License; the `OFL-*.txt` files
  go with them.
- The day page in the hero is drawn in HTML. Its content is an example. Never put real data on this page.
