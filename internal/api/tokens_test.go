package api

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTheTokensHoldTheirContrast reads the one token block of base.css, where each colour is written once as
// light-dark(light, dark), and checks each pair that a token's use names (docs/plans/096-bound-in-hearthwood.md):
// text 4.5:1 on its material (ink on parchment, the page's ink on oak, the rail's ink on wood), an edge, the outline
// and the focus ring 3:1. The ratio is WCAG 2's, computed here and not by the application. The APCA lightness
// contrast of every text pair is logged beside it and fails nothing (docs/plans/097-the-almanac-reflects.md).
func TestTheTokensHoldTheirContrast(t *testing.T) {
	css := cssComments.ReplaceAllString(string(baseCSS), "")
	light, dark := tokenBlock(t, css, regexp.MustCompile(`(?s)
:root\{(.*?)
\}`))
	type pair struct {
		fg, bg string
		min    float64
		text   bool
	}
	var pairs []pair
	text := func(fg string, grounds ...string) {
		for _, bg := range grounds {
			pairs = append(pairs, pair{fg, bg, 4.5, true})
		}
	}
	mark := func(fg string, grounds ...string) {
		for _, bg := range grounds {
			pairs = append(pairs, pair{fg, bg, 3, false})
		}
	}
	text("ink", "surface", "surface-deep", "accent-soft", "mark", "warn-soft", "done-soft", "danger-soft")
	text("muted", "surface", "surface-deep")
	text("page-ink", "ground")
	text("page-strong", "ground")
	text("page-muted", "ground")
	text("rail-ink", "rail", "rail-deep")
	text("rail-accent", "rail", "rail-deep")
	text("teal", "rail", "rail-deep")   // a tool's name on its row of wood (docs/plans/104-the-chat-in-basm.md)
	text("indigo", "rail", "rail-deep") // the owner's nameplate
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
	themes := []struct {
		name   string
		tokens map[string]string
	}{{"light", light}, {"dark", dark}}
	for _, th := range themes {
		for _, p := range pairs {
			fg, bg := resolve(t, th.tokens, p.fg), resolve(t, th.tokens, p.bg)
			if r := contrast(t, fg, bg); r < p.min {
				t.Errorf("%s: --%s %s on --%s %s is %.2f:1, want %.1f:1 or more", th.name, p.fg, fg, p.bg, bg, r, p.min)
			}
		}
	}
	// APCA (version 0.1.9) is advisory: it is logged, and it fails nothing. Read the log with -v. The Lc of a text
	// pair is read against the table below (the sign tells light text on a dark ground, minus, from dark text on a
	// light ground, plus).
	//
	//	|Lc| 90  preferred for body text
	//	|Lc| 75  the least for body text
	//	|Lc| 60  the floor for large or fluent text
	//	|Lc| 45  large bold text
	//	|Lc| 30  spot text
	for _, th := range themes {
		for _, p := range pairs {
			if !p.text {
				continue
			}
			fg, bg := resolve(t, th.tokens, p.fg), resolve(t, th.tokens, p.bg)
			t.Logf("apca %s: --%s on --%s Lc %.1f", th.name, p.fg, p.bg, apca(t, fg, bg))
		}
	}
}

// tokenBlock is the custom properties of the one block the pattern finds: name (without --) to the light value and
// to the dark value. A light-dark(a, b) gives a to the light map and b to the dark map; any other value goes to
// both. Two equal values in a light-dark() fail: the token is written once.
func tokenBlock(t *testing.T, css string, block *regexp.Regexp) (light, dark map[string]string) {
	t.Helper()
	m := block.FindStringSubmatch(css)
	if m == nil {
		t.Fatalf("base.css has no block %s", block)
	}
	light, dark = map[string]string{}, map[string]string{}
	pairRe := regexp.MustCompile(`^light-dark\(\s*([^,()]+?)\s*,\s*([^,()]+?)\s*\)$`)
	for _, d := range regexp.MustCompile(`--([a-z0-9-]+):([^;]+);`).FindAllStringSubmatch(m[1], -1) {
		name, v := d[1], strings.TrimSpace(d[2])
		if p := pairRe.FindStringSubmatch(v); p != nil {
			if p[1] == p[2] {
				t.Errorf("--%s is light-dark(%s, %s): the two values are equal, write it once as the colour", name, p[1], p[2])
			}
			light[name], dark[name] = p[1], p[2]
			continue
		}
		light[name], dark[name] = v, v
	}
	return light, dark
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

// apca is the APCA-W3 0.1.9 lightness contrast Lc of text colour fg on ground bg, both #rrggbb: positive for dark
// text on a light ground, negative for light text on a dark one. The constants are the published ones.
func apca(t *testing.T, fg, bg string) float64 {
	const (
		normBG, normTXT, revTXT, revBG = 0.56, 0.57, 0.62, 0.65
		blkThrs, blkClmp               = 0.022, 1.414
		scaleBoW, scaleWoB             = 1.14, 1.14
		loBoWoffset, loWoBoffset       = 0.027, 0.027
		loClip, deltaYmin              = 0.1, 0.0005
	)
	luminance := func(c string) float64 {
		var y float64
		for i, k := range [3]float64{0.2126729, 0.7151522, 0.0721750} {
			n, err := strconv.ParseUint(c[1+2*i:3+2*i], 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			y += k * math.Pow(float64(n)/255, 2.4)
		}
		if y < blkThrs {
			y += math.Pow(blkThrs-y, blkClmp)
		}
		return y
	}
	yt, yb := luminance(fg), luminance(bg)
	if math.Abs(yb-yt) < deltaYmin {
		return 0
	}
	if yb > yt {
		c := (math.Pow(yb, normBG) - math.Pow(yt, normTXT)) * scaleBoW
		if c < loClip {
			return 0
		}
		return (c - loBoWoffset) * 100
	}
	c := (math.Pow(yb, revBG) - math.Pow(yt, revTXT)) * scaleWoB
	if c > -loClip {
		return 0
	}
	return (c + loWoBoffset) * 100
}
