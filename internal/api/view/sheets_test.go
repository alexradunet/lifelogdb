package view

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"lifelog/internal/pixelart"
)

// TestTheBaseSheetCarriesNoAppVocabulary keeps the two sheets apart: base.css styles ordinary HTML, and this
// app's own names live in app.css alone, so a page reads as HTML with either file alone. Comments are cut first:
// the rule is about selectors, not about prose that names one.
func TestTheBaseSheetCarriesNoAppVocabulary(t *testing.T) {
	base := cssComments.ReplaceAllString(string(baseCSS), "")
	for _, name := range []string{".wikilink", ".badge", ".chart", ".pager", ".plain", ".prose", ".sub", ".skip",
		".tiny", ".inline", ".danger", ".quiet", ".desc", ".group", ".scroll", ".brand",
		".calendar", ".mood", ".more", ".here"} {
		if strings.Contains(base, name) {
			t.Errorf("base.css styles %s, which belongs to app.css: the base sheet is ordinary HTML", name)
		}
	}
	app := cssComments.ReplaceAllString(string(appCSS), "")
	for _, want := range []string{"a.wikilink", "svg.chart", ".badge", "nav.pager", "form.tiny"} {
		if !strings.Contains(app, want) {
			t.Errorf("app.css lost %s: the app's own vocabulary has no home", want)
		}
	}
	for _, want := range []string{"table", "label", "button", "details", "article", "aside", "@media print"} {
		if !strings.Contains(base, want) {
			t.Errorf("base.css does not style %s: ordinary HTML would be bare", want)
		}
	}
}

// cssComments cuts a stylesheet's comments, so a test reads its rules and not its prose.
var cssComments = regexp.MustCompile(`(?s)/\*.*?\*/`)

// TestTheSheetsAreLayered: the two sheets are one cascade in four layers (docs/plans/108-the-browser-made-current.md),
// declared once at the top of base.css before anything but the fonts, which stay outside the layers; app.css holds
// the two last layers and never repeats the declaration, which would be a second home for the order.
func TestTheSheetsAreLayered(t *testing.T) {
	base := strings.TrimSpace(cssComments.ReplaceAllString(string(baseCSS), ""))
	app := cssComments.ReplaceAllString(string(appCSS), "")
	if !strings.HasPrefix(base, "@layer tokens, base, components, utilities;") {
		t.Errorf("base.css does not start with the layer order: %.80q", base)
	}
	before := base[:strings.Index(base, "@layer tokens{")]
	if rest := regexp.MustCompile(`(?s)@layer [^;]+;|@font-face\{[^}]*\}|\s`).ReplaceAllString(before, ""); rest != "" {
		t.Errorf("base.css has a rule outside the layers before the tokens: %.80q", rest)
	}
	for _, want := range []string{"@layer tokens{", "@layer base{"} {
		if strings.Count(base, want) != 1 {
			t.Errorf("base.css opens %s %d times, want once", want, strings.Count(base, want))
		}
	}
	for _, want := range []string{"@layer components{", "@layer utilities{"} {
		if strings.Count(app, want) != 1 {
			t.Errorf("app.css opens %s %d times, want once", want, strings.Count(app, want))
		}
	}
	if strings.Contains(app, "@layer tokens") || strings.Contains(app, "@layer base") || regexp.MustCompile(`@layer [^{]*;`).MatchString(app) {
		t.Error("app.css declares a layer order or a layer of base.css")
	}
	// A rule outside the layers beats every layer, the utilities too: in app.css every block at the top is one of its
	// two layers (docs/plans/119-the-finish.md, rule 8).
	for _, head := range topBlocks(app) {
		if head != "@layer components" && head != "@layer utilities" {
			t.Errorf("app.css has a rule outside its layers: %.60q", head)
		}
	}
}

// topBlocks is the head of each block at the top of a sheet with no comments, and the text between blocks that is not
// space, so a declaration outside any block is reported too.
func topBlocks(sheet string) []string {
	var heads []string
	depth, start := 0, 0
	for i, r := range sheet {
		switch r {
		case '{':
			if depth == 0 {
				heads = append(heads, strings.TrimSpace(sheet[start:i]))
			}
			depth++
		case '}':
			depth--
			if depth == 0 {
				start = i + 1
			}
		}
	}
	if rest := strings.TrimSpace(sheet[start:]); rest != "" {
		heads = append(heads, rest)
	}
	return heads
}

// TestTheSheetsCloseEveryBlock: every block of a sheet is closed, so no rule is nested by accident in the rule
// before it. A brace lost with a deleted line once put every rule after .metric-card inside it, where it matched
// nothing.
func TestTheSheetsCloseEveryBlock(t *testing.T) {
	for name, sheet := range map[string]string{"base.css": string(baseCSS), "app.css": string(appCSS)} {
		depth, reported := 0, false
		for i, line := range strings.Split(cssComments.ReplaceAllStringFunc(sheet, func(c string) string { return strings.Repeat("\n", strings.Count(c, "\n")) }), "\n") {
			// A line that starts at the margin with a selector or an at-rule starts a rule of a layer, or of the
			// sheet itself: the blocks before it are closed.
			if line != "" && !strings.ContainsAny(line[:1], " \t}") && depth > 1 && !reported {
				t.Errorf("%s:%d: %.60q starts inside %d open blocks", name, i+1, line, depth)
				reported = true
			}
			depth += strings.Count(line, "{") - strings.Count(line, "}")
		}
		if depth != 0 {
			t.Errorf("%s ends with %d blocks open", name, depth)
		}
	}
}

// TestTheSheetsUseLogicalProperties: a rule says inline and block, start and end, never left, right, top and
// bottom, so the sheets read the same in any writing direction. A value stays physical where the property is a
// position of a background or a shadow; only a property name is checked here.
func TestTheSheetsUseLogicalProperties(t *testing.T) {
	physical := []*regexp.Regexp{
		regexp.MustCompile(`(^|[{;\s])(margin|padding|border)-(left|right|top|bottom)(-[a-z]+)?\s*:`),
		regexp.MustCompile(`(^|[{;\s])(left|right|top|bottom|width|height|min-width|max-width|min-height|max-height)\s*:`),
		regexp.MustCompile(`(^|[{;\s])(text-align|float)\s*:\s*(left|right)\b`),
	}
	for name, sheet := range map[string]string{"base.css": string(baseCSS), "app.css": string(appCSS)} {
		rules := cssComments.ReplaceAllString(sheet, "")
		for _, re := range physical {
			for _, m := range re.FindAllString(rules, -1) {
				t.Errorf("%s uses a physical property: %q", name, strings.TrimSpace(m))
			}
		}
	}
}

// TestTheSheetsQueryTheColumnNotTheScreen: a layout inside the page asks the width of its column (@container
// column), not of the screen, so it is right beside an open sheet and in a narrow window alike; a width media query
// stays only where the screen is the subject: the phone's bars and the touch size of a control (899 px), the shell's
// own columns and the top layer. The title and the sheet scale with the column by clamp(), not by a step.
func TestTheSheetsQueryTheColumnNotTheScreen(t *testing.T) {
	widths := func(sheet string) string {
		seen := map[string]bool{}
		var out []string
		for _, m := range regexp.MustCompile(`@media \(max-width:(\d+px)\)`).FindAllStringSubmatch(cssComments.ReplaceAllString(sheet, ""), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
		return strings.Join(out, " ")
	}
	if got := widths(string(baseCSS)); got != "899px" {
		t.Errorf("base.css asks the screen's width at %s, want 899px alone", got)
	}
	if got := widths(string(appCSS)); got != "899px" {
		t.Errorf("app.css asks the screen's width at %s, want 899px alone", got)
	}
	app := cssComments.ReplaceAllString(string(appCSS), "")
	if n := strings.Count(app, "@container column ("); n < 8 {
		t.Errorf("app.css asks its column %d times, want at least 8", n)
	}
	if !strings.Contains(app, "container:column / inline-size") {
		t.Error("app.css does not make the column a container")
	}
	base := cssComments.ReplaceAllString(string(baseCSS), "")
	for _, want := range []string{"h1{font:400 clamp(", "h2{font-size:clamp(", "@container (max-width:608px)"} {
		if !strings.Contains(base, want) {
			t.Errorf("base.css lacks %s", want)
		}
	}
	for _, want := range []string{".leaf{flex:none; display:flex; flex-direction:column; inline-size:clamp(", "--sheet-gap:clamp("} {
		if !strings.Contains(app, want) {
			t.Errorf("app.css lacks %s", want)
		}
	}
}

// TestTheMotionsAreOffUnderReducedMotion: every entry, exit and scroll-driven motion of the sheets is inside a block
// that asks for motion (prefers-reduced-motion:no-preference), and the scroll-driven one inside @supports as well,
// so a browser that does not want motion, or cannot drive it by the scroll, gets none.
func TestTheMotionsAreOffUnderReducedMotion(t *testing.T) {
	for name, sheet := range map[string]string{"base.css": string(baseCSS), "app.css": string(appCSS)} {
		t.Run(name, func(t *testing.T) { motionsAskFirst(t, cssComments.ReplaceAllString(sheet, ""), name == "app.css") })
	}
}

// motionsAskFirst checks one sheet; scroll says the sheet must hold the scroll-driven animation.
func motionsAskFirst(t *testing.T, app string, scroll bool) {
	// enclosing is the preludes of the blocks around the index, outermost first.
	enclosing := func(at int) []string {
		var stack []string
		start := 0
		for i := 0; i < at; i++ {
			switch app[i] {
			case '{':
				stack = append(stack, strings.TrimSpace(app[start:i]))
				start = i + 1
			case '}':
				stack = stack[:len(stack)-1]
				start = i + 1
			case ';':
				start = i + 1
			}
		}
		return stack
	}
	within := func(stack []string, want string) bool {
		for _, p := range stack {
			if strings.Contains(p, want) {
				return true
			}
		}
		return false
	}
	for _, token := range []string{"@starting-style", "allow-discrete", "animation-timeline:scroll(root)"} {
		if !strings.Contains(app, token) && (scroll || token != "animation-timeline:scroll(root)") {
			t.Errorf("the sheet has no %s", token)
		}
		for at := strings.Index(app, token); at >= 0; at = strings.Index(app, token) {
			stack := enclosing(at)
			if !within(stack, "prefers-reduced-motion:no-preference") {
				t.Errorf("%s at %q is outside a block that asks for motion", token, strings.Join(stack, " > "))
			}
			if strings.HasPrefix(token, "animation-timeline:") && !within(stack, "@supports (animation-timeline:scroll())") {
				t.Errorf("%s at %q is outside @supports", token, strings.Join(stack, " > "))
			}
			app = app[:at] + strings.Repeat(" ", len(token)) + app[at+len(token):]
		}
	}
}

// TestTheFieldsSayWhatTheBrowserRefuses: ordinary HTML, so base.css: a field the browser refuses (:user-invalid)
// and one the server refused (aria-invalid) share one rule, and the select is the app's own where a sheet may draw
// it (appearance:base-select), behind @supports so the native one stays elsewhere.
func TestTheFieldsSayWhatTheBrowserRefuses(t *testing.T) {
	base := cssComments.ReplaceAllString(string(baseCSS), "")
	app := cssComments.ReplaceAllString(string(appCSS), "")
	for _, want := range []string{":is(input,textarea,select):is([aria-invalid=true],:user-invalid){border-color:var(--danger-line)}",
		"@supports (appearance:base-select){", "::picker(select){", "select::picker-icon{"} {
		if !strings.Contains(base, want) {
			t.Errorf("base.css lacks %s", want)
		}
	}
	if strings.Contains(app, "aria-invalid") || strings.Contains(app, "base-select") {
		t.Error("app.css styles a field's refusal or the select: ordinary HTML belongs to base.css")
	}
	if strings.Count(base, "base-select") != 2 {
		t.Errorf("base.css names base-select %d times, want the @supports and the one declaration", strings.Count(base, "base-select"))
	}
}

// TestAClosedToastIsNotShown: the toast is a dialog, and a closed dialog is the browser's display:none, which a
// display on the element itself would beat; so the sheet sets the toast's display only while it is open (the
// owner's Close once closed the dialog and left the box in view).
func TestAClosedToastIsNotShown(t *testing.T) {
	app := cssComments.ReplaceAllString(string(appCSS), "")
	if !strings.Contains(app, ".toast[open]{display:flex}") {
		t.Error("app.css does not tie the toast's display to its open state")
	}
	for _, m := range regexp.MustCompile(`\.toast\{[^{}]*`).FindAllString(app, -1) {
		if strings.Contains(m, "display:") {
			t.Errorf("app.css sets the toast's display outside [open]: %.80s", m)
		}
	}
}

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
	text("ink", "surface", "surface-deep", "accent-soft", "mark", "danger-soft")
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
	text("warn-ink", "surface")
	text("done", "surface")
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

// TestThePicturesAreTheDrawings: each html/*.png picture is the output of its drawing in pixelart, not an edited
// picture. The pixels are compared and not the bytes, because the PNG encoder may change. tools/pixelart writes the
// files.
func TestThePicturesAreTheDrawings(t *testing.T) {
	for _, c := range []struct {
		file string
		want *image.NRGBA
	}{{"wood.png", pixelart.Wood()}, {"ground.png", pixelart.Ground()}, {"sprites.png", pixelart.Sprites()}} {
		got, err := png.Decode(bytes.NewReader(MustFile(c.file)))
		if err != nil {
			t.Fatal(err)
		}
		if got.Bounds() != c.want.Bounds() {
			t.Fatalf("%s is %v, want %v", c.file, got.Bounds(), c.want.Bounds())
		}
		b := c.want.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if g, w := color.NRGBAModel.Convert(got.At(x, y)), c.want.At(x, y); g != w {
					t.Fatalf("%s: pixel (%d,%d) is %v, want %v: run go run ./tools/pixelart", c.file, x, y, g, w)
				}
			}
		}
	}
}

// TestTheSheetReadsTheSpriteCellsWhereTheyAre: the numbers in app.css (the size of the sheet, the offset of a cell) are
// a copy of the layout of the sprite sheet, and a CSS file cannot read a Go constant. This test keeps the copy equal:
// the seal and the faces at 3x, the inkwell at 2x.
func TestTheSheetReadsTheSpriteCellsWhereTheyAre(t *testing.T) {
	app := string(appCSS)
	const seal, inkwell = 3, 2
	want := []string{
		fmt.Sprintf("0 0/%dpx %dpx", pixelart.Cells*pixelart.CellSize*seal, pixelart.CellSize*seal),
		fmt.Sprintf("-%dpx 0/%dpx %dpx", pixelart.CellInkwell*pixelart.CellSize*inkwell, pixelart.Cells*pixelart.CellSize*inkwell, pixelart.CellSize*inkwell),
	}
	for n := 1; n <= 5; n++ { // the face of the mood n is the cell CellFace+n-1
		want = append(want, fmt.Sprintf(`input[value="%d"]) span::before{background-position:-%dpx 0}`, n, (pixelart.CellFace+n-1)*pixelart.CellSize*seal))
	}
	for _, w := range want {
		if !strings.Contains(app, w) {
			t.Errorf("app.css lacks %q, which the layout of the sprite sheet needs", w)
		}
	}
}
