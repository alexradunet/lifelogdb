package importer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// The owner's review of a gate (docs/guides/importing.md, "Gates"): approve shows the lines changed since the
// last approval, from a copy of the approved file the writer keeps when it stamps. The copy serves the review
// only; the gate is the stamp's hash (Gate), never the copy.

// approvedDir holds that copy, one file per stamped file. Only Approve writes it. No model operation can: every
// other workspace write is a fixed file name or a facts path, and a facts path refuses a hidden folder.
const approvedDir = ".approved"

func (w *Workspace) approvedPath(name string) string { return filepath.Join(w.Dir, approvedDir, name) }

// lastApproved is the stamp line and body of the file's last approval, from the copy. A copy whose stamp does
// not cover its own body, or that is not the approval the file's own stamp still names, is not used.
func (w *Workspace) lastApproved(name string) (stamp, body string, ok bool) {
	b, err := os.ReadFile(w.approvedPath(name))
	if err != nil {
		return "", "", false
	}
	stamp, body = splitStatus(strings.ReplaceAll(string(b), "\r\n", "\n"))
	if m := stampRE.FindStringSubmatch(stamp); m == nil || m[2] != bodyHash(body) {
		return "", "", false
	}
	if text, ok, _ := w.read(name); ok {
		if cur, _ := splitStatus(text); stampRE.MatchString(cur) && cur != stamp {
			return "", "", false
		}
	}
	return stamp, body, true
}

// Review is what the owner reads before approving a file.
type Review struct {
	File  string
	Since string // the day of the last approval the diff is against; "" when there is none: Text is then the whole body
	Text  string // display-only diff or whole body; terminal controls are visibly escaped, newlines and tabs preserved
	Hash  string // the sha256 of the body Approve will stamp: it stamps nothing else
}

// Review prepares the owner's review of rules.md or metrics.md: the body that approving it stamps (in
// metrics.md every row still proposed reads approved), as a diff against the last approval.
func (w *Workspace) Review(name string) (*Review, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	rest, err := w.toStamp(name)
	if err != nil {
		return nil, err
	}
	r := &Review{File: name, Text: rest, Hash: bodyHash(rest)}
	if stamp, body, ok := w.lastApproved(name); ok {
		r.Since = stampRE.FindStringSubmatch(stamp)[1]
		r.Text = strings.Join(unified(body, rest, 2), "\n")
	}
	r.Text = reviewDisplay(r.Text)
	return r, nil
}

func reviewDisplay(text string) string {
	var b strings.Builder
	for _, r := range text {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || r == '\u061c' || r == '\u200e' || r == '\u200f' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			if r <= 0xff {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Changed is the short diff status shows for a gate that is not approved: the changed lines since the last
// approval, at most max of them, or nil when there is no usable copy or no line changed.
func (w *Workspace) Changed(name string, max int) []string {
	text, ok, err := w.read(name)
	if err != nil || !ok {
		return nil
	}
	_, body, ok := w.lastApproved(name)
	if !ok {
		return nil
	}
	_, rest := splitStatus(text)
	d := unified(body, rest, 0)
	if len(d) > max {
		d = append(d[:max:max], fmt.Sprintf("… %d more diff lines: approving shows them all", len(d)-max))
	}
	return d
}

// toStamp is the body approving a file would stamp.
func (w *Workspace) toStamp(name string) (string, error) {
	if name != "rules.md" && name != "metrics.md" && name != entitiesFile && name != preparedFile && name != selectedPhotoFile {
		return "", fmt.Errorf("approve rules.md, metrics.md, entities.md, prepared.md or selected-photo.md, not %s", name)
	}
	rest, err := w.artifactBody(name, false)
	if err != nil {
		return "", err
	}
	if name == preparedFile {
		if _, err := w.parsePrepared(context.Background(), rest); err != nil {
			return "", err
		}
	}
	if name == selectedPhotoFile {
		if _, _, err := w.parseSelectedPhoto(context.Background(), rest); err != nil {
			return "", err
		}
	}
	if name == entitiesFile {
		if err := checkEntities(rest); err != nil {
			return "", err
		}
	}
	if name == "metrics.md" || name == entitiesFile {
		rest = approveRows(rest)
	}
	return rest, nil
}

// ---- a line diff, unified style

type edit struct {
	op     byte // ' ' kept, '-' removed, '+' added
	ai, bi int  // old and new lines before this one
	text   string
}

// lineDiff is a shortest edit script between two line lists: common head and tail trimmed, then a longest
// common subsequence over the rest (a very large rest is shown as removed whole and added whole).
func lineDiff(a, b []string) []edit {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	var out []edit
	ai, bi := 0, 0
	emit := func(op byte, text string) {
		out = append(out, edit{op, ai, bi, text})
		if op != '+' {
			ai++
		}
		if op != '-' {
			bi++
		}
	}
	for _, l := range a[:pre] {
		emit(' ', l)
	}
	n, m := len(ma), len(mb)
	if n*m > 4_000_000 {
		for _, l := range ma {
			emit('-', l)
		}
		for _, l := range mb {
			emit('+', l)
		}
	} else {
		// lcs[i][j]: the longest common subsequence of ma[i:] and mb[j:]
		lcs := make([][]int32, n+1)
		for i := range lcs {
			lcs[i] = make([]int32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if ma[i] == mb[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < n || j < m {
			switch {
			case i < n && j < m && ma[i] == mb[j]:
				emit(' ', ma[i])
				i, j = i+1, j+1
			case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]): // removals before additions
				emit('-', ma[i])
				i++
			default:
				emit('+', mb[j])
				j++
			}
		}
	}
	for _, l := range a[len(a)-suf:] {
		emit(' ', l)
	}
	return out
}

// unified renders the line diff of two bodies as hunks: a header `@@ -line,count +line,count @@` in the
// numbering of the whole file (the status line is line 1), then each line marked ' ', '-' or '+', with context
// unchanged lines around each change. No change, no hunk.
func unified(old, new string, context int) []string {
	es := lineDiff(strings.Split(old, "\n"), strings.Split(new, "\n"))
	var changes []int
	for i, e := range es {
		if e.op != ' ' {
			changes = append(changes, i)
		}
	}
	var out []string
	for k := 0; k < len(changes); {
		first, last := changes[k], changes[k]
		for k++; k < len(changes) && changes[k]-last <= 2*context+1; k++ {
			last = changes[k]
		}
		from, to := max(0, first-context), min(len(es), last+context+1)
		oldN, newN := 0, 0
		for _, e := range es[from:to] {
			if e.op != '+' {
				oldN++
			}
			if e.op != '-' {
				newN++
			}
		}
		out = append(out, fmt.Sprintf("@@ -%s +%s @@", span(es[from].ai, oldN), span(es[from].bi, newN)))
		for _, e := range es[from:to] {
			out = append(out, string(e.op)+e.text)
		}
	}
	return out
}

// span is a hunk range: before is the body lines before it, and the file's status line comes first.
func span(before, n int) string {
	start := before + 2 // 1-based, after the status line
	if n == 0 {
		start-- // an empty range names the line it follows
	}
	if n == 1 {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d,%d", start, n)
}
