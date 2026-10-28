package api_test

// The browser's side of plan 146: the Settings page, the conversation view and the chat column of every page.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// chatScript is the one script element a page may carry.
const chatScript = `<script src="/assets/chat.js" defer></script>`

// chatColumn is the chat column of a page: the popover #chat, up to the end of its form.
var chatColumn = regexp.MustCompile(`(?s)<div class="chat" id="chat".*?</form>\s*</div>`)

// withoutChat is a page without its chat column, for the tests that look at the forms of the page itself.
func withoutChat(page string) string { return chatColumn.ReplaceAllString(page, "") }

// TestTheSettingsPageDrawsTheThreeFieldsInOneForm: the model URL, the model id and the key are fields of one form
// whose one loud button is Save; the key is a password input that holds its value; a save is a redirect back to
// /settings with the toast; the generic view's list of properties, the key among them, is not drawn.
func TestTheSettingsPageDrawsTheThreeFieldsInOneForm(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	page := browse(t, h, "/settings")
	form := regexp.MustCompile(`(?s)<form method="POST" action="/settings">.*?</form>`).FindString(page)
	for _, want := range []string{`<input type="text" name="model_url" value="">`, `<input type="text" name="model" value="">`,
		`<input type="password" name="model_key" value="">`, `<button class="primary">Save</button>`} {
		if !strings.Contains(form, want) {
			t.Errorf("the form of /settings lacks %s:\n%s", want, form)
		}
	}
	if strings.Count(withoutChat(page), `class="primary"`) != 1 {
		t.Errorf("/settings has %d loud buttons beside the chat column, want 1", strings.Count(withoutChat(page), `class="primary"`))
	}
	rec := submit(h, "/settings", url.Values{"model_url": {"http://127.0.0.1:1234/v1"}, "model": {"synthetic-model"}, "model_key": {"synthetic-key"}}, "/settings")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings?feedback=") {
		t.Fatalf("POST /settings: %d to %q, want 303 to /settings with a receipt", rec.Code, rec.Header().Get("Location"))
	}
	page = browse(t, h, rec.Header().Get("Location"))
	for _, want := range []string{`class="toast"`, "Settings saved.", `<input type="text" name="model_url" value="http://127.0.0.1:1234/v1">`,
		`<input type="text" name="model" value="synthetic-model">`, `<input type="password" name="model_key" value="synthetic-key">`} {
		if !strings.Contains(page, want) {
			t.Errorf("/settings after a save lacks %s", want)
		}
	}
	if strings.Contains(page, "<h2>Properties</h2>") {
		t.Error("/settings lists its properties, the key among them")
	}
}

// TestTheRailHasChatAndSettings: Chat is a section of the rail and goes to the conversation of today; Settings is
// beside Snapshot in the owner's line. Each is marked on its own page.
func TestTheRailHasChatAndSettings(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	body := browse(t, h, "/browse")
	for _, want := range []string{`<a href="/days/today/conversation"><span class="icon icon-chat" aria-hidden="true"></span>Chat</a>`,
		` · <a href="/settings">Settings</a> · <a href="/actions?new=snapshot">Snapshot</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the rail lacks %s", want)
		}
	}
	if !strings.Contains(browse(t, h, "/days/today/conversation"), `<a href="/days/today/conversation" aria-current="page">`) {
		t.Error("Chat is not marked on the conversation")
	}
	if !strings.Contains(browse(t, h, "/days/"+fixedToday+"/conversation"), `<a href="/days/today/conversation" aria-current="page">`) {
		t.Error("Chat is not marked on the conversation of a day by its date")
	}
	if !strings.Contains(browse(t, h, "/settings"), `<a href="/settings" aria-current="page">Settings</a>`) {
		t.Error("Settings is not marked on /settings")
	}
}

// TestTheConversationViewDrawsTheThreadTheComposerAndTheMargin: the turns in order, the owner's and the agent's told
// apart by class, the tool lines of an agent's turn, markdown in the words, a free paragraph as it is, the composer
// of say with its day in the address, the margin of the page, and no chat column.
func TestTheConversationViewDrawsTheThreadTheComposerAndTheMargin(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	conv := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation"))
	if _, err := c.Do(t.Context(), apitest.Action(t, conv, "say"), map[string]string{"text": "How did I **sleep**?"}); err != nil {
		t.Fatal(err)
	}
	page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")), "page")))
	body := page.Properties.(map[string]any)["body"].(string)
	body += "\n\n- `search_pages` words=sleep\n- `read_day` day=" + fixedToday + "\n\n**lifelog** (09.30): You slept *well*.\n\nA note the owner typed.\n"
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "save-body"), map[string]string{"body": body}))

	view := browse(t, h, "/days/"+fixedToday+"/conversation")
	order := []string{`<li class="turn owner">`, `How did I <strong>sleep</strong>?`, `<li class="turn lifelog">`, "search_pages", "read_day", `You slept <em>well</em>.`, `<li class="turn text">`, "A note the owner typed."}
	at := 0
	for _, want := range order {
		i := strings.Index(view[at:], want)
		if i < 0 {
			t.Fatalf("the conversation view lacks %q after byte %d:\n%s", want, at, view)
		}
		at += i
	}
	for _, want := range []string{`<ul class="tools">`, `<form method="POST" action="/days/` + fixedToday + `/say">`, `<textarea name="text" rows="3" required>`,
		`<button class="primary">Say</button>`, `<input type="hidden" name="back" value="/days/` + fixedToday + `/conversation">`,
		`<summary>Edit</summary>`, `aria-label="Categories"`, `aria-label="Facts"`, `aria-label="More"`} {
		if !strings.Contains(view, want) {
			t.Errorf("the conversation view lacks %s", want)
		}
	}
	if strings.Contains(view, `id="chat"`) || strings.Contains(view, "<script") || strings.Contains(view, `class="waiting"`) {
		t.Error("the conversation view draws the chat column, the script, or a waiting line (the last turn is the agent's)")
	}
	if strings.Contains(view, `class="page-body"`) {
		t.Error("the body is drawn twice: as the thread and as the page")
	}
}

// TestADayWithNoTurnsSaysWhereTheAgentAnswers: the empty state names the way forward.
func TestADayWithNoTurnsSaysWhereTheAgentAnswers(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	view := browse(t, h, "/days/"+fixedToday+"/conversation")
	for _, want := range []string{`class="empty"`, "It answers here when it runs", "lifelog whatsapp", `<textarea name="text"`} {
		if !strings.Contains(view, want) {
			t.Errorf("the empty conversation lacks %q", want)
		}
	}
	if strings.Contains(view, `class="thread"`) || strings.Contains(view, `aria-label="Facts"`) {
		t.Error("a day with no page draws a thread or the margin of a page")
	}
}

// TestEveryPageOfTheShellDrawsTheChatColumn: the column holds today's turns, a waiting line when the last turn is
// the owner's, a form of say to today whose back is the page, and the version of the conversation page; the script
// is loaded once, with defer. The composer posts, and the page comes back to where it was.
func TestEveryPageOfTheShellDrawsTheChatColumn(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	day := "/days/" + fixedToday
	empty := browse(t, h, day)
	for _, want := range []string{`<div class="chat" id="chat" popover role="complementary" data-version="" aria-label="Chat">`, "Nothing is said today yet",
		`<form method="POST" action="/days/today/say" class="chat-form">`, `<input type="hidden" name="back" value="` + day + `">`,
		`popovertarget="chat"`, chatScript} {
		if !strings.Contains(empty, want) {
			t.Errorf("the page of a day with no talk lacks %s", want)
		}
	}
	if strings.Count(empty, `<script`) != 1 || strings.Contains(empty, `class="waiting"`) || strings.Contains(chatColumn.FindString(empty), "Preview") {
		t.Error("the column draws more than one script, a waiting line with no turn, or a Preview button")
	}

	rec := submit(h, "/days/today/say", url.Values{"text": {"Anyone there?"}, "back": {day}}, day)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), day+"?feedback=") {
		t.Fatalf("POST /days/today/say with back: %d to %q, want 303 to the page", rec.Code, rec.Header().Get("Location"))
	}
	conv := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	version := conv.Properties.(map[string]any)["version"].(string)
	page := browse(t, h, day)
	column := chatColumn.FindString(page)
	for _, want := range []string{`data-version="` + version + `"`, `<li class="turn owner">`, "Anyone there?", `<p class="waiting"`} {
		if !strings.Contains(column, want) {
			t.Errorf("the column after a turn lacks %s:\n%s", want, column)
		}
	}
	if !strings.Contains(browse(t, h, "/browse"), `<input type="hidden" name="back" value="/browse">`) {
		t.Error("the form of the column on /browse does not return to /browse")
	}
	if strings.Contains(browse(t, h, "/days/today/conversation"), `id="chat"`) {
		t.Error("the conversation view draws the column")
	}
}

// TestChatJSIsServedAsJavaScript: the one script is a short file, served as the icons are, with a short cache.
func TestChatJSIsServedAsJavaScript(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/chat.js", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age=") {
		t.Errorf("GET /assets/chat.js: %d %v", rec.Code, rec.Header())
	}
	if lines := strings.Count(rec.Body.String(), "\n"); lines >= 80 {
		t.Errorf("chat.js has %d lines, want under 80", lines)
	}
	if !strings.Contains(rec.Body.String(), "application/vnd.siren+json") || !strings.Contains(rec.Body.String(), "/days/today/conversation") {
		t.Error("chat.js does not poll the conversation of today")
	}
}
