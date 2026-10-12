package api

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTheTokensHoldTheirContrast reads the two token blocks of base.css, light and dark, and checks each pair that a
// token's use names (docs/plans/096-bound-in-hearthwood.md): text 4.5:1 on its material (ink on parchment, the page's
// ink on oak, the rail's ink on wood), an edge, the outline and the focus ring 3:1. The ratio is WCAG 2's, computed
// here and not by the application.
func TestTheTokensHoldTheirContrast(t *testing.T) {
	css := cssComments.ReplaceAllString(string(baseCSS), "")
	light := tokenBlock(t, css, regexp.MustCompile(`(?s)\n:root\{(.*?)\n\}`))
	dark := tokenBlock(t, css, regexp.MustCompile(`(?s)@media \(prefers-color-scheme:dark\)\{:root\{(.*?)\}\}`))
	for name := range light {
		if strings.HasPrefix(light[name], "#") {
			if _, ok := dark[name]; !ok {
				t.Errorf("--%s has no dark value", name)
			}
		}
	}
	for name, v := range light { // the aliases (focus, toast-bg, toast-ink) follow their token in dark
		if _, ok := dark[name]; !ok {
			dark[name] = v
		}
	}
	type pair struct {
		fg, bg string
		min    float64
	}
	var pairs []pair
	text := func(fg string, grounds ...string) {
		for _, bg := range grounds {
			pairs = append(pairs, pair{fg, bg, 4.5})
		}
	}
	mark := func(fg string, grounds ...string) {
		for _, bg := range grounds {
			pairs = append(pairs, pair{fg, bg, 3})
		}
	}
	text("ink", "surface", "surface-deep", "accent-soft", "mark", "warn-soft", "done-soft", "danger-soft")
	text("muted", "surface", "surface-deep")
	text("page-ink", "ground")
	text("page-strong", "ground")
	text("page-muted", "ground")
	text("rail-ink", "rail", "rail-deep")
	text("rail-accent", "rail", "rail-deep")
	text("accent", "ground")
	text("accent-ink", "surface", "surface-deep")
	text("link", "surface", "surface-deep")
	text("link-page", "ground")
	text("on-accent", "accent", "primary", "primary-hover")
	text("warn", "surface")
	text("warn-ink", "warn-soft")
	text("done", "surface")
	text("done-ink", "done-soft")
	text("danger", "surface")
	text("danger-ink", "danger-soft")
	text("toast-ink", "toast-bg")
	text("toast-link", "toast-bg")
	mark("edge", "surface")
	mark("outline", "surface", "surface-deep")
	mark("danger-line", "surface")
	mark("focus", "ground", "rail", "surface")
	for theme, tokens := range map[string]map[string]string{"light": light, "dark": dark} {
		for _, p := range pairs {
			fg, bg := resolve(t, tokens, p.fg), resolve(t, tokens, p.bg)
			if r := contrast(t, fg, bg); r < p.min {
				t.Errorf("%s: --%s %s on --%s %s is %.2f:1, want %.1f:1 or more", theme, p.fg, fg, p.bg, bg, r, p.min)
			}
		}
	}
}

// tokenBlock is the custom properties of the block the pattern finds: name (without --) to value.
func tokenBlock(t *testing.T, css string, block *regexp.Regexp) map[string]string {
	t.Helper()
	m := block.FindStringSubmatch(css)
	if m == nil {
		t.Fatalf("base.css has no block %s", block)
	}
	out := map[string]string{}
	for _, d := range regexp.MustCompile(`--([a-z0-9-]+):([^;]+);`).FindAllStringSubmatch(m[1], -1) {
		out[d[1]] = strings.TrimSpace(d[2])
	}
	return out
}

// resolve follows var(--x) to a colour.
func resolve(t *testing.T, tokens map[string]string, name string) string {
	t.Helper()
	v := tokens[name]
	for i := 0; i < 8 && strings.HasPrefix(v, "var(--"); i++ {
		v = tokens[strings.TrimSuffix(strings.TrimPrefix(v, "var(--"), ")")]
	}
	if !regexp.MustCompile(`^#[0-9a-f]{6}$`).MatchString(v) {
		t.Fatalf("--%s is %q, not a colour #rrggbb", name, v)
	}
	return v
}

// contrast is the WCAG 2 contrast ratio of two colours #rrggbb.
func contrast(t *testing.T, a, b string) float64 {
	lum := func(c string) float64 {
		var l [3]float64
		for i := range l {
			n, err := strconv.ParseUint(c[1+2*i:3+2*i], 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			s := float64(n) / 255
			if s <= 0.03928 {
				l[i] = s / 12.92
			} else {
				l[i] = math.Pow((s+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
	}
	x, y := lum(a), lum(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}
