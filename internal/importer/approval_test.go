package importer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// ownerApproves is the CLI's approve without the terminal: the owner reads the review, then stamps what it showed.
func ownerApproves(w *Workspace, name string) error {
	rv, err := w.Review(name)
	if err != nil {
		return err
	}
	return w.Approve(name, time.Now(), rv.Hash)
}

// editLine replaces one line of a workspace file, as an editor would.
func editLine(t *testing.T, w *Workspace, name, from, to string) {
	t.Helper()
	p := filepath.Join(w.Dir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), from) {
		t.Fatalf("%s holds no %q", name, from)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// changes are the removed and added lines of a diff, without its headers and context.
func changes(diff []string) []string {
	var out []string
	for _, l := range diff {
		if !strings.HasPrefix(l, "@@") && (strings.HasPrefix(l, "-") || strings.HasPrefix(l, "+")) {
			out = append(out, l)
		}
	}
	return out
}

const (
	folderLine  = "- Journal/*.md — one note per day" // line 5 of rules.md: status, source, blank, ## Folders
	folderLine2 = "- Journal/*.md — one note per date"
)

func TestFirstApprovalShowsTheWholeFile(t *testing.T) {
	f := setup(t)
	if err := f.w.DraftRules(rulesBody); err != nil {
		t.Fatal(err)
	}
	rv, err := f.w.Review("rules.md")
	if err != nil {
		t.Fatal(err)
	}
	if rv.Since != "" || rv.Text != rulesBody {
		t.Errorf("the first review is not the whole body: since %q, text %q", rv.Since, rv.Text)
	}
	if err := f.w.Approve("rules.md", time.Now(), rv.Hash); err != nil {
		t.Fatal(err)
	}
	if g, _ := f.w.Gate("rules.md"); g != "approved" {
		t.Errorf("after approve: %s", g)
	}
}

func TestAStaleGateSaysWhatChanged(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	editLine(t, f.w, "rules.md", folderLine, folderLine2)
	if g, _ := f.w.Gate("rules.md"); g != "stale" {
		t.Fatalf("one edited word leaves the gate %s", g)
	}

	// status names the changed line
	st, err := f.w.Status(ctx, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	d := st.Changed["rules.md"]
	if want := []string{"@@ -5 +5 @@", "-" + folderLine, "+" + folderLine2}; !slices.Equal(d, want) {
		t.Errorf("status shows %q, want %q", d, want)
	}
	if !strings.Contains(st.DoNow, "rules.md is stale (changed since the approval: line 5)") {
		t.Errorf("do now does not name the line: %s", st.DoNow)
	}

	// approving again shows that change only, with a little context
	rv, err := f.w.Review("rules.md")
	if err != nil {
		t.Fatal(err)
	}
	if rv.Since != time.Now().Format(time.DateOnly) {
		t.Errorf("the review is against %q, not today's approval", rv.Since)
	}
	diff := strings.Split(rv.Text, "\n")
	if got, want := changes(diff), []string{"-" + folderLine, "+" + folderLine2}; !slices.Equal(got, want) {
		t.Errorf("the review changes %q, want %q", got, want)
	}
	if diff[0] != "@@ -3,5 +3,5 @@" || strings.Contains(rv.Text, "Bob Sample") {
		t.Errorf("the review is not one hunk around line 5:\n%s", rv.Text)
	}
	if err := f.w.Approve("rules.md", time.Now(), rv.Hash); err != nil {
		t.Fatal(err)
	}
	if g, _ := f.w.Gate("rules.md"); g != "approved" {
		t.Errorf("after the second approve: %s", g)
	}
	if rv, _ := f.w.Review("rules.md"); rv.Since == "" || rv.Text != "" {
		t.Errorf("unchanged since the approval, the review shows %q", rv.Text)
	}
	if st, _ := f.w.Status(ctx, nil, ""); st.Changed != nil {
		t.Errorf("an approved gate lists changes: %q", st.Changed)
	}
}

func TestAnAddedMetricIsNamed(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	if err := f.w.ProposeMetric(Metric{Name: "mood", Note: "1-5"}); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, "metrics.md"); err != nil {
		t.Fatal(err)
	}
	if err := f.w.ProposeMetric(Metric{Name: "ferritin", Unit: "ng/mL"}); err != nil {
		t.Fatal(err)
	}
	st, err := f.w.Status(ctx, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := changes(st.Changed["metrics.md"])
	if len(got) != 1 || !strings.HasPrefix(got[0], "+| proposed | ferritin |") {
		t.Errorf("status shows %q, want the added row only", st.Changed["metrics.md"])
	}
	rv, _ := f.w.Review("metrics.md")
	if got := changes(strings.Split(rv.Text, "\n")); len(got) != 1 || !strings.HasPrefix(got[0], "+| approved | ferritin |") {
		t.Errorf("the review shows %q, want the row as it will be stamped", got)
	}
}

func TestTheApprovedCopyNeverOpensTheGate(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	editLine(t, f.w, "rules.md", folderLine, folderLine2)
	copyPath := f.w.approvedPath("rules.md")

	// a copy forged to match the edited body, with a stamp of its own: the gate is the file's stamp, so it stays
	// closed, and the copy is not the approval the file's stamp names, so the review falls back to the whole file
	text, _, _ := f.w.read("rules.md")
	_, edited := splitStatus(text)
	forged := "status: approved 2031-01-01 (owner) sha256:" + bodyHash(edited) + "\n" + edited
	if err := os.WriteFile(copyPath, []byte(forged), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, _ := f.w.Gate("rules.md"); g != "stale" {
		t.Errorf("a forged copy leaves the gate %s", g)
	}
	if rv, _ := f.w.Review("rules.md"); rv.Since != "" || rv.Text != edited {
		t.Errorf("a forged copy hides the change: since %q, text %q", rv.Since, rv.Text)
	}

	// a copy edited in place no longer matches its own stamp: not used either
	b, _ := os.ReadFile(f.w.approvedPath("rules.md"))
	os.WriteFile(copyPath, []byte(strings.Replace(string(b), "date", "day", 1)), 0o644)
	if rv, _ := f.w.Review("rules.md"); rv.Since != "" {
		t.Errorf("an edited copy is used for the review")
	}
	if d := f.w.Changed("rules.md", 20); d != nil {
		t.Errorf("status uses an edited copy: %q", d)
	}

	// the model's operations cannot write the copy: a facts path refuses a hidden folder
	for _, p := range []string{".approved/rules.md", "../.approved/rules.md", "x/../../.approved/rules"} {
		if _, err := f.w.factsPath(p); err == nil {
			t.Errorf("a facts path %q reaches the approved copies", p)
		}
	}
}

func TestApproveStampsOnlyWhatWasShown(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	editLine(t, f.w, "rules.md", folderLine, folderLine2)
	rv, err := f.w.Review("rules.md")
	if err != nil {
		t.Fatal(err)
	}
	editLine(t, f.w, "rules.md", "## Distinct", "## Distinct\n- \"Cara\" ≠ \"Cara Example\"")
	if err := f.w.Approve("rules.md", time.Now(), rv.Hash); err == nil {
		t.Error("a body changed after the review was stamped")
	}
	if g, _ := f.w.Gate("rules.md"); g != "stale" {
		t.Errorf("after a refused approve: %s", g)
	}
}

func TestUnified(t *testing.T) {
	for _, c := range []struct {
		old, new string
		want     []string
	}{
		{"a\nb\nc\n", "a\nb\nc\n", nil},
		{"a\nb\nc\n", "a\nB\nc\n", []string{"@@ -3 +3 @@", "-b", "+B"}},
		{"a\nb\nc\n", "a\nb\nc\nd\n", []string{"@@ -4,0 +5 @@", "+d"}},
		{"a\nb\nc\n", "a\nc\n", []string{"@@ -3 +2,0 @@", "-b"}},
		{"a\nb\nc\nd\ne\nf\n", "A\nb\nc\nd\ne\nF\n", []string{"@@ -2 +2 @@", "-a", "+A", "@@ -7 +7 @@", "-f", "+F"}},
	} {
		if got := unified(c.old, c.new, 0); !slices.Equal(got, c.want) {
			t.Errorf("unified(%q, %q) = %q, want %q", c.old, c.new, got, c.want)
		}
	}
	if got := changedLines([]string{"@@ -3 +3 @@", "-b", "+B", "@@ -4,0 +5,2 @@", "+d", "+e", "@@ -6 +7,0 @@", "-x"}); got != " (changed since the approval: line 3, lines 5-6, removed after line 7)" {
		t.Errorf("changedLines: %q", got)
	}
}
