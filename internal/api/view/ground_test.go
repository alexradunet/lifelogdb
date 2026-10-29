package view

import (
	"regexp"
	"strings"
	"testing"
)

// TestTheGroundIsTheOwnersChoice: app.css has a rule for each floor that differs from the boards (oak, firelight and
// embers; the boards are the body's own rule, so a page with no choice has them), and the two moving floors animate
// only inside the block that asks for motion.
func TestTheGroundIsTheOwnersChoice(t *testing.T) {
	app := cssComments.ReplaceAllString(string(appCSS), "")
	for _, ground := range []string{"oak", "firelight", "embers"} {
		if !strings.Contains(app, "[data-ground="+ground+"]") {
			t.Errorf("app.css has no rule for the ground %s", ground)
		}
	}
	// A rule of a moving ground that sets animation: must sit directly inside the motion block, which is the one
	// block that holds "{" before it with no "}" between.
	motion := regexp.MustCompile(`@media \(prefers-reduced-motion:no-preference\)\{`)
	for _, at := range regexp.MustCompile(`data-ground=(?:firelight|embers)\]::(?:before|after)\{animation:`).FindAllStringIndex(app, -1) {
		open := motion.FindAllStringIndex(app[:at[0]], -1)
		if len(open) == 0 {
			t.Fatalf("a moving ground animates outside a block that asks for motion: %q", app[at[0]:at[1]])
		}
		last := open[len(open)-1][1]
		if strings.Count(app[last:at[0]], "{") != strings.Count(app[last:at[0]], "}") {
			t.Errorf("a moving ground animates outside the motion block: %q", app[at[0]:at[1]])
		}
	}
}
