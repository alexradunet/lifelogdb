package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/importer"
)

// importCatalog is the import's operations (docs/guides/importing.md, "The writer's operations"). It is part
// of GET /actions, and so of the CLI and the MCP tools, only when the process was started with a workspace.
// approve is not here at all: it is the owner's, at a terminal (lifelog import approve).
var importCatalog = []spec{
	{"import-status", "Import status", "Start every turn here: the gates, the ledger, the questions, the next file, one 'do now' sentence and any mismatch. Do what it says.",
		"GET", "/import", nil, false},
	{"make-ledger", "Make the ledger", "Write ledger.md: every file of the source, each to do. Run again, it adds the files added to the source since and changes no line.",
		"POST", "/import/ledger", nil, false},
	{"skip-file", "Skip files", "Mark a ledger file skipped for good, with the reason a rule gives (a view file, not a life log); or a pattern (Drive/**, **/*.canvas): every file still to do that it matches.",
		"POST", "/import/skip", []Field{req("file", "text", "File or pattern"), req("reason", "text", "Reason")}, false},
	{"defer-file", "Hold files for a later pass", "Mark a ledger file, or every file still to do that a pattern matches (**/*.pdf, **/*.jpg), as later: an attachment to keep as a file with its text, a photo to select. Not now, not never.",
		"POST", "/import/defer", []Field{req("file", "text", "File or pattern"), req("reason", "text", "Reason")}, false},
	{"inspect", "Inspect a file", "Read one source file's structure: frontmatter, headings, tables as rows, checkboxes, links (or a CSV's rows, or the text).",
		"GET", "/import/inspect", []Field{req("file", "text", "File (relative to the source)")}, false},
	{"import-find", "Find", "Look a name up before writing it: people, places, pages and metrics that match exactly, with the same words, more words or fewer words.",
		"GET", "/import/find", []Field{req("text", "text", "Name")}, false},
	{"draft-rules", "Draft rules", "Write rules.md for the owner to approve: a `source: import:<name>` line, then ## Folders, ## Aliases (\"name\" → \"Title\"), ## Distinct (\"a\" ≠ \"b\"), ## Decisions. Never a status line; any change waits for the owner's approval again.",
		"POST", "/import/rules", []Field{req("body", "textarea", "rules.md without its status line")}, false},
	{"propose-metric", "Propose a metric", "Add a proposed metric to metrics.md for the owner: its name, the title its page will have for good (reuse one that exists), the unit exactly as written ('' when the source writes none; a scale's range such as '1-5', which no source line writes, is its unit: reuse Mood with '' or '1-5'; a habit is unitless with note '1 = done that day'); category, the path of the category to file it in (biomarkers/iron), when metrics.md has that column.",
		"POST", "/import/metrics", []Field{req("name", "text", "Name"), opt("unit", "text", "Unit"), opt("note", "text", "Note"), opt("from", "text", "From file"), opt("doubts", "text", "Doubts"), opt("category", "text", "Category")}, false},
	{"ask", "Ask the owner", "Add a question to questions.md: what you found and options that each say the exact rows its answer writes (one option per line). Never answer it yourself.",
		"POST", "/import/questions", []Field{req("question", "text", "Question"), req("options", "textarea", "Options, one per line"), opt("file", "text", "File and line"), opt("about", "text", "About"), opt("found", "text", "Found")}, false},
	{"close-question", "Close a question", "Mark an answered question done, once its answer is in the rules and the facts and applied.",
		"POST", "/import/questions/{id}/done", []Field{path("id", "text", "Question (Q<n>)")}, false},
	{"write-facts", "Write facts", "Store the facts file of one source file: JSON {file, writes:[{person|place|page|link|reading, quote}], waiting, kept_as_text} as the guide describes. Then check it.",
		"POST", "/import/facts", []Field{req("file", "text", "File"), req("facts", "textarea", "Facts (JSON)")}, false},
	{"check-facts", "Check facts", "Run every check of a stored facts file and show what apply would write, writing nothing.",
		"POST", "/import/facts/check", []Field{req("file", "text", "File")}, false},
	{"apply-facts", "Apply facts", "Check and write a file's facts in one transaction, then its ledger line.",
		"POST", "/import/facts/apply", []Field{req("file", "text", "File")}, false},
	{"draft-prepared", "Draft supported source", "Derive a bounded fixed-profile batch for owner review. Bindings/mappings are JSON; this cannot approve it.", "POST", "/import/prepared", []Field{req("file", "text", "Source file"), req("profile", "text", "Fixed profile"), opt("kind", "text", "Session kind"), opt("binding", "textarea", "Fit owner-assigned identity/day/activity"), req("metrics", "textarea", "Quantity code to metric handle JSON")}, false},
	{"check-prepared", "Check prepared source", "Validate the stamped source-derived batch and roll all writes back.", "POST", "/import/prepared/check", nil, false},
	{"apply-prepared", "Apply prepared source", "Apply the owner-stamped source-derived batch atomically, then publish ledger.", "POST", "/import/prepared/apply", nil, false},
	{"draft-selected-photo", "Draft selected photo pair", "Review an explicit original/optional sidecar pair, exact fingerprints, capture/creation evidence and choices. No automatic precedence.", "POST", "/import/selected-photo", []Field{req("file", "text", "Original"), opt("sidecar", "text", "Explicit sidecar"), req("title", "text", "Title"), opt("body", "textarea", "Caption"), req("choices", "textarea", "Capture/GPS choices and optional owner day/offset/place JSON")}, false},
	{"check-selected-photo", "Check selected photo", "Check the owner-stamped pair without writes.", "POST", "/import/selected-photo/check", nil, false},
	{"apply-selected-photo", "Apply selected photo", "Keep the stamped selected pair through the existing file writer.", "POST", "/import/selected-photo/apply", nil, false},
	{"register-metrics", "Register metrics", "Register the metrics the owner approved in metrics.md, and each habit's period.",
		"POST", "/import/register-metrics", nil, false},
	{"plan-vault", "Plan the vault", "Draft plan.json: every note becomes one page titled by its file name; every problem is listed.",
		"POST", "/import/vault/plan", nil, false},
	{"fix-plan", "Fix the plan", "Change one note's title or day in plan.json (the only fields you may change).",
		"POST", "/import/vault/fix", []Field{req("path", "text", "Note path"), opt("title", "text", "Title"), opt("day", "date", "Day")}, false},
	{"apply-vault", "Apply the vault", "Create every note's page, then save each note's text through the save contract (a daily note of an existing day is appended once).",
		"POST", "/import/vault/apply", nil, false},
	{"replay", "Replay", "Apply the whole workspace to another database (the real run): only when the owner says so. It is rehearsed on a throwaway copy of the target first and writes nothing unless the rehearsal is clean; dry_run=1 is the rehearsal alone, listing every failure.",
		"POST", "/import/replay", []Field{req("to", "text", "Target database path"),
			{Name: "dry_run", Type: "number", Title: "Dry run: rehearse on a copy, write nothing", Options: []string{"1", "0"}}}, true},
}

func (h *server) mountImport(get func(string, func(*http.Request) (*Entity, error)), post func(string, func(*http.Request, string) (*Entity, error))) {
	get("/import", h.importStatus)
	get("/import/inspect", h.inspect)
	get("/import/find", h.importFind)
	get("/import/questions", h.questions)
	get("/import/ledger", h.ledger)
	get("/import/plan", h.plan)
	get("/import/facts", h.facts)
	get("/import/prepared", h.preparedReview)
	get("/import/selected-photo", h.selectedPhotoReview)
	post("/import/ledger", h.makeLedger)
	post("/import/skip", h.markFiles("-"))
	post("/import/defer", h.markFiles(">"))
	post("/import/rules", h.draftRules)
	post("/import/metrics", h.proposeMetric)
	post("/import/questions", h.ask)
	post("/import/questions/{id}/done", h.closeQuestion)
	post("/import/facts", h.writeFacts)
	post("/import/facts/check", h.factsOp(false))
	post("/import/facts/apply", h.factsOp(true))
	post("/import/prepared", h.draftPrepared)
	post("/import/prepared/check", h.preparedOp(true))
	post("/import/prepared/apply", h.preparedOp(false))
	post("/import/selected-photo", h.draftSelectedPhoto)
	post("/import/selected-photo/check", h.selectedPhotoOp(true))
	post("/import/selected-photo/apply", h.selectedPhotoOp(false))
	post("/import/register-metrics", h.registerMetrics)
	post("/import/vault/plan", h.planVault)
	post("/import/vault/fix", h.fixPlan)
	post("/import/vault/apply", h.applyVault)
	post("/import/replay", ownerOnly(h.replay))
}

func importLinks(self string) []Link {
	return []Link{link("self", self, "This"), link("status", "/import", "Import status"), link("ledger", "/import/ledger", "Ledger"),
		link("questions", "/import/questions", "Questions"), link("plan", "/import/plan", "Vault plan"),
		link("prepared-review", "/import/prepared", "Prepared source review"),
		link("selected-photo-review", "/import/selected-photo", "Selected photo review"), link("index", "/", "Home")}
}

func (h *server) importEntity(class, title, self string, props any, next ...string) *Entity {
	e := &Entity{Class: []string{"import", class}, Title: title, Properties: props, Links: importLinks(self)}
	for _, n := range next {
		e.Actions = append(e.Actions, action(n, nil, nil))
	}
	return e
}

func (h *server) dbPath(r *http.Request) string {
	res, err := h.s.Query(r.Context(), `SELECT file FROM pragma_database_list WHERE name = 'main'`, 1)
	if err != nil || len(res.Rows) == 0 {
		return ""
	}
	s, _ := res.Rows[0][0].(string)
	return s
}

func (h *server) importStatus(r *http.Request) (*Entity, error) {
	st, err := h.ws.Status(r.Context(), h.s, h.dbPath(r))
	if err != nil {
		return nil, err
	}
	e := h.importEntity("status", "Import status", "/import", st)
	if st.Next != "" {
		e.Actions = append(e.Actions, action("inspect", nil, map[string]any{"file": st.Next}),
			action("write-facts", nil, map[string]any{"file": st.Next}),
			action("check-facts", nil, map[string]any{"file": st.Next}),
			action("apply-facts", nil, map[string]any{"file": st.Next}))
	}
	return e, nil
}

func (h *server) inspect(r *http.Request) (*Entity, error) {
	file := r.URL.Query().Get("file")
	in, err := h.ws.Inspect(file)
	if err != nil {
		return nil, err
	}
	return h.importEntity("inspection", file, "/import/inspect?file="+url.QueryEscape(file), in), nil
}

func (h *server) importFind(r *http.Request) (*Entity, error) {
	q := r.URL.Query().Get("text")
	if strings.TrimSpace(q) == "" {
		return nil, &core.Error{Status: 422, Msg: "missing field text"}
	}
	ms, err := importer.Find(r.Context(), h.s, q)
	if err != nil {
		return nil, err
	}
	e := h.importEntity("find", "Find "+q, "/import/find?text="+url.QueryEscape(q), map[string]any{"text": q, "matches": ms})
	for _, m := range ms {
		e.Entities = append(e.Entities, Link{Rel: []string{"match", strings.ReplaceAll(m.Relation, " ", "-")}, Href: m.Href, Title: m.Title, Class: []string{m.Kind}})
	}
	return e, nil
}

func (h *server) questions(*http.Request) (*Entity, error) {
	qs, err := h.ws.Questions()
	if err != nil {
		return nil, err
	}
	return h.importEntity("questions", "Questions", "/import/questions", map[string]any{"questions": qs}, "ask"), nil
}

func (h *server) ledger(*http.Request) (*Entity, error) {
	lines, _, err := h.ws.Ledger()
	if err != nil {
		return nil, err
	}
	return h.importEntity("ledger", "Ledger", "/import/ledger", map[string]any{"files": lines}, "skip-file", "defer-file"), nil
}

func (h *server) plan(*http.Request) (*Entity, error) {
	p, ok, err := h.ws.LoadPlan()
	if err != nil {
		return nil, err
	}
	if !ok {
		return h.importEntity("plan", "Vault plan", "/import/plan", map[string]any{"plan": nil}, "plan-vault"), nil
	}
	return h.importEntity("plan", "Vault plan", "/import/plan", p, "fix-plan", "apply-vault"), nil
}

func (h *server) facts(r *http.Request) (*Entity, error) {
	file := r.URL.Query().Get("file")
	f, err := h.ws.LoadFacts(file)
	if err != nil {
		return nil, err
	}
	e := h.importEntity("facts", file, "/import/facts?file="+url.QueryEscape(file), f)
	e.Actions = []Action{action("check-facts", nil, map[string]any{"file": file}), action("apply-facts", nil, map[string]any{"file": file})}
	return e, nil
}

func (h *server) makeLedger(r *http.Request, _ string) (*Entity, error) {
	res, err := h.ws.RefreshLedger()
	if err != nil {
		return nil, err
	}
	e, err := h.ledger(r)
	if e != nil {
		e.Result = res
	}
	return e, err
}

// markFiles is skip-file ("-") and defer-file (">"): one file, or every file still to do that a pattern matches.
func (h *server) markFiles(state string) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, _ string) (*Entity, error) {
		v, err := form(r)
		if err != nil {
			return nil, err
		}
		if err := required(v, "file", "reason"); err != nil {
			return nil, err
		}
		n, err := h.ws.MarkFiles(v.Get("file"), state, v.Get("reason"))
		if err != nil {
			return nil, err
		}
		e, err := h.importStatus(r)
		if e != nil {
			e.Result = map[string]any{"marked": n}
		}
		return e, err
	}
}

func (h *server) draftRules(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := h.ws.DraftRules(v.Get("body")); err != nil {
		return nil, err
	}
	return h.importStatus(r)
}

func (h *server) proposeMetric(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := h.ws.ProposeMetric(importer.Metric{Name: v.Get("name"), Unit: v.Get("unit"), Note: v.Get("note"),
		From: v.Get("from"), Doubts: v.Get("doubts"), Category: v.Get("category")}); err != nil {
		return nil, err
	}
	ms, err := h.ws.Metrics()
	if err != nil {
		return nil, err
	}
	return h.importEntity("metrics", "metrics.md", "/import", map[string]any{"metrics": ms}, "propose-metric"), nil
}

func (h *server) ask(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	var opts []string
	for _, o := range strings.Split(v.Get("options"), "\n") {
		if o = strings.TrimSpace(o); o != "" {
			opts = append(opts, o)
		}
	}
	id, err := h.ws.Ask(importer.Question{File: v.Get("file"), About: v.Get("about"), Question: v.Get("question")}, v.Get("found"), opts)
	if err != nil {
		return nil, err
	}
	e, err := h.questions(r)
	if e != nil {
		e.Result = map[string]any{"asked": id}
	}
	return e, err
}

func (h *server) closeQuestion(r *http.Request, _ string) (*Entity, error) {
	if err := h.ws.CloseQuestion(r.PathValue("id")); err != nil {
		return nil, err
	}
	return h.questions(r)
}

func (h *server) writeFacts(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "file", "facts"); err != nil {
		return nil, err
	}
	if err := h.ws.WriteFacts(v.Get("file"), []byte(v.Get("facts"))); err != nil {
		return nil, err
	}
	r2 := r.Clone(r.Context())
	r2.URL.RawQuery = "file=" + url.QueryEscape(v.Get("file"))
	return h.facts(r2)
}

func (h *server) factsOp(apply bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, _ string) (*Entity, error) {
		v, err := form(r)
		if err != nil {
			return nil, err
		}
		if err := required(v, "file"); err != nil {
			return nil, err
		}
		var rep *importer.Report
		if apply {
			rep, err = h.ws.Apply(r.Context(), h.s, v.Get("file"))
		} else {
			rep, err = h.ws.Check(r.Context(), h.s, v.Get("file"))
		}
		if err != nil {
			return nil, err
		}
		title := "Check " + rep.File
		next := []string{"apply-facts"}
		if apply {
			title, next = "Applied "+rep.File, []string{"import-status"}
		}
		e := h.importEntity("report", title, "/import", rep)
		for _, n := range next {
			e.Actions = append(e.Actions, action(n, nil, map[string]any{"file": rep.File}))
		}
		return e, nil
	}
}

func (h *server) registerMetrics(r *http.Request, _ string) (*Entity, error) {
	done, err := h.ws.RegisterMetrics(r.Context(), h.s)
	if err != nil {
		return nil, err
	}
	return h.importEntity("metrics", "Metrics registered", "/import", map[string]any{"registered": done}, "import-status"), nil
}

func (h *server) planVault(r *http.Request, _ string) (*Entity, error) {
	before, _, err := h.ws.LoadPlan()
	if err != nil {
		return nil, err
	}
	p, err := h.ws.PlanVault(r.Context(), h.s)
	if err != nil {
		return nil, err
	}
	e, err := h.plan(r)
	if e != nil {
		added := len(p.Notes)
		if before != nil {
			added -= len(before.Notes)
		}
		e.Result = map[string]any{"notes": len(p.Notes), "added": added}
	}
	return e, err
}

func (h *server) fixPlan(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "path"); err != nil {
		return nil, err
	}
	if _, err := h.ws.FixPlan(r.Context(), h.s, v.Get("path"), v.Get("title"), v.Get("day")); err != nil {
		return nil, err
	}
	return h.plan(r)
}

func (h *server) applyVault(r *http.Request, _ string) (*Entity, error) {
	res, err := h.ws.ApplyVault(r.Context(), h.s)
	if err != nil {
		return nil, err
	}
	return h.importEntity("vault", "Vault applied", "/import", res, "import-status"), nil
}

func (h *server) replay(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "to"); err != nil {
		return nil, err
	}
	switch v.Get("dry_run") {
	case "", "0":
	case "1":
		res, err := h.ws.Rehearse(r.Context(), h.s, v.Get("to"))
		if err != nil {
			return nil, err
		}
		title := "Rehearsed on a copy of " + v.Get("to") + ": clean, nothing written"
		if len(res.Failures) > 0 {
			title = fmt.Sprintf("Rehearsed on a copy of %s: %d failures, nothing written", v.Get("to"), len(res.Failures))
		}
		return h.importEntity("replay", title, "/import", res), nil
	default:
		return nil, &core.Error{Status: 422, Msg: "dry_run is 1 or 0"}
	}
	res, err := h.ws.Replay(r.Context(), h.s, v.Get("to"))
	if err != nil {
		return nil, err
	}
	return h.importEntity("replay", "Replayed into "+v.Get("to"), "/import", res), nil
}

func (h *server) draftPrepared(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err = required(v, "file", "profile", "metrics"); err != nil {
		return nil, err
	}
	b, err := h.ws.DraftPreparedJSON(r.Context(), v.Get("file"), v.Get("profile"), v.Get("kind"), v.Get("binding"), v.Get("metrics"))
	if err != nil {
		return nil, err
	}
	return h.importEntity("prepared", "Draft supported source", "/import/prepared", b), nil
}
func (h *server) preparedOp(dry bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, _ string) (*Entity, error) {
		rep, err := h.ws.Prepared(r.Context(), h.s, dry)
		if err != nil {
			return nil, err
		}
		return h.importEntity("report", "Prepared source", "/import", rep), nil
	}
}

func (h *server) draftSelectedPhoto(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err = required(v, "file", "title", "choices"); err != nil {
		return nil, err
	}
	p, err := h.ws.DraftSelectedPhotoJSON(r.Context(), v.Get("file"), v.Get("sidecar"), v.Get("title"), v.Get("body"), v.Get("choices"))
	if err != nil {
		return nil, err
	}
	return h.importEntity("selected-photo", "Draft selected pair", "/import/selected-photo", p), nil
}
func (h *server) selectedPhotoOp(dry bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, _ string) (*Entity, error) {
		rep, err := h.ws.SelectedPhoto(r.Context(), h.s, dry)
		if err != nil {
			return nil, err
		}
		return h.importEntity("report", "Selected photo", "/import", rep), nil
	}
}

func (h *server) preparedReview(r *http.Request) (*Entity, error) {
	b, err := h.ws.PreparedReview(r.Context())
	if err != nil {
		return nil, err
	}
	return h.importEntity("prepared", "Source interpretation (owner stamp required)", "/import/prepared", b, "check-prepared", "apply-prepared"), nil
}
func (h *server) selectedPhotoReview(r *http.Request) (*Entity, error) {
	p, err := h.ws.SelectedPhotoReview(r.Context())
	if err != nil {
		return nil, err
	}
	return h.importEntity("selected-photo", "Selected claims and choices (owner stamp required)", "/import/selected-photo", p, "check-selected-photo", "apply-selected-photo"), nil
}
