package api_test

// The browser's side of plan 146: the Settings page, the conversation view and the chat column of every page.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/client"
)

// chatScript is the one script element a page may carry.
const chatScript = `<script src="/assets/chat.js" defer></script>`

// chatColumn is the chat column of a page: the popover #chat, up to the end of its form.
var chatColumn = regexp.MustCompile(`(?s)<div class="chat compact" id="chat".*?</form>\s*<p class="hint">.*?</p>\s*</div>`)

// withoutChat is a page without its chat column, for the tests that look at the forms of the page itself.
func withoutChat(page string) string { return chatColumn.ReplaceAllString(page, "") }

// TestTheSettingsPageDrawsTheThreeFieldsInOneForm: the model URL, the model id and the key are fields of one form
// whose one loud button is Save; the key is a password input that holds its value; a save is a redirect back to
// /settings with the toast; the generic view's list of properties, the key among them, is not drawn.
func TestTheSettingsPageDrawsTheThreeFieldsInOneForm(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	page := browse(t, h, "/settings")
	form := regexp.MustCompile(`(?s)<form method="POST" action="/settings" autocomplete="off">.*?</form>`).FindString(page)
	for _, want := range []string{`<input type="text" name="model_url" value="">`, `<input type="text" name="model" value="">`, `<input type="text" name="owner_number" value="">`,
		`<input type="password" name="model_key" value="" autocomplete="off">`, `<button class="primary">Save</button>`} {
		if !strings.Contains(form, want) {
			t.Errorf("the form of /settings lacks %s:\n%s", want, form)
		}
	}
	if strings.Count(withoutChat(page), `class="primary"`) != 1 {
		t.Errorf("/settings has %d loud buttons beside the chat column, want 1", strings.Count(withoutChat(page), `class="primary"`))
	}
	rec := submit(h, "/settings", url.Values{"model_url": {"http://127.0.0.1:1234/v1"}, "model": {"synthetic-model"}, "model_key": {"synthetic-key"}, "owner_number": {"40712345678"}}, "/settings")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings?feedback=") {
		t.Fatalf("POST /settings: %d to %q, want 303 to /settings with a receipt", rec.Code, rec.Header().Get("Location"))
	}
	page = browse(t, h, rec.Header().Get("Location"))
	for _, want := range []string{`class="toast"`, "Settings saved.", `<input type="text" name="model_url" value="http://127.0.0.1:1234/v1">`,
		`<input type="text" name="model" value="synthetic-model">`, `<input type="text" name="owner_number" value="40712345678">`, `<input type="password" name="model_key" value="synthetic-key" autocomplete="off">`} {
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

// screenOf is the chat screen of a page, for a message.
func screenOf(page string) string {
	i := strings.Index(page, `<div class="chat-screen"`)
	if i < 0 {
		return page[:min(len(page), 300)]
	}
	return page[i:min(len(page), i+6000)]
}

// conversationBody is the body of today's conversation page as an agent would write it into the page with save-body:
// the body that the API gives, then more text.
func conversationBody(t *testing.T, c *client.Client, more string) *siren.Entity {
	t.Helper()
	conv := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation"))
	page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(conv, "page")))
	body := page.Properties.(map[string]any)["body"].(string)
	return apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "save-body"), map[string]string{"body": body + more}))
}

// TestTheConversationViewIsTheChatScreen: the conversation is a screen and not a page. The day bar names the day with
// "today", has the chips of the last three days that have a conversation page and Today (the current one marked),
// and a menu with the page's own things; there is no page head, no margin and no categories row outside the menu.
// The thread has the turns in order, the owner's and the agent's told apart by class, the relay of a turn, the tool
// lines as chips, markdown and wikilinks in the words and a free paragraph as it is; the composer is the form of say
// and returns to the screen; three dots wait while the last turn is the owner's. The column is not drawn.
func TestTheConversationViewIsTheChatScreen(t *testing.T) {
	c, h, s := apitest.FreshOn(t, "cli", fixedToday)
	for _, day := range []string{"2031-05-01", "2031-05-10", "2031-05-12", "2031-05-13"} { // the oldest is not one of the last three
		if _, _, err := s.CreatePage(t.Context(), "cli", "Conversation "+day, "[["+day+"]]\n", ""); err != nil {
			t.Fatal(err)
		}
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")), "say"), map[string]string{"text": "How did I **sleep**? See [[Sleep notes]]."}))
	conversationBody(t, c, "\n\n- `search_pages` words=sleep\n- `read_day` failed: not_found\n\n**lifelog** (09.30): You slept *well*.\n")
	relay := apitest.Remote(t, h, "agent:whatsapp")
	apitest.Must(t)(relay.Do(t.Context(), apitest.Action(t, apitest.Must(t)(relay.Get(t.Context(), "/days/today/conversation")), "say"), map[string]string{"text": "And last night?"}))
	body := apitest.Must(t)(c.Get(t.Context(), apitest.Href(apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")), "page"))).Properties.(map[string]any)["body"].(string)
	body = strings.Replace(body, "**Owner**", "A note the owner typed.\n\n**Owner**", 1) // text before the first marker is a text turn
	conv := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, conv, "save-body"), map[string]string{"body": body}))

	if _, _, err := s.CreatePage(t.Context(), "cli", "Sleep log", "I talked it over in [[Conversation "+fixedToday+"]].\n", ""); err != nil { // a page that links the conversation
		t.Fatal(err)
	}
	view := browse(t, h, "/days/"+fixedToday+"/conversation")
	order := []string{`<h1>Chat</h1><span class="day">Wed 14 May · today</span>`,
		`<nav class="days" aria-label="Days"><a href="/days/2031-05-10/conversation">Sat 10</a><a href="/days/2031-05-12/conversation">Mon 12</a><a href="/days/2031-05-13/conversation">Tue 13</a><a href="/days/today/conversation" aria-current="page">Today</a></nav>`,
		`<button type="button" class="more" popovertarget="chat-menu"`, `<div class="more-body" id="chat-menu" popover>`, `popovertargetaction="hide">Close</button>`,
		`the day view</a>`, `aria-label="Categories"`, `<summary>Edit</summary>`, `Plan a line`, `aria-label="Links"`, `aria-label="Backlinks"`, `class="danger">Delete`, `<section class="thread"`,
		`<li class="turn text">`, "A note the owner typed.",
		`<li class="turn owner">`, `<span class="from">You</span><time>`, `How did I <strong>sleep</strong>?`, `<a href="/pages?title=Sleep+notes" class="wikilink">Sleep notes</a>`,
		`<li class="turn agent">`, `<span class="from">lifelog</span>`, `<li><b>search_pages</b> words=sleep</li>`, `<li><b>read_day</b> failed: not_found</li>`, `You slept <em>well</em>.`,
		`<li class="turn owner">`, `<span class="from">You</span><span class="via">· via whatsapp</span><time>`, "And last night?",
		`<div class="thinking" role="status"`, `<footer class="composer">`}
	at := 0
	for _, want := range order {
		i := strings.Index(view[at:], want)
		if i < 0 {
			t.Fatalf("the chat screen lacks %q after byte %d:\n%s", want, at, screenOf(view))
		}
		at += i
	}
	for _, gone := range []string{`class="page-head"`, `page-margin`, `aria-label="Facts"`, `id="chat"`, `<details class="more-menu"`, `class="page-body"`, `class="waiting"`} {
		if strings.Contains(view, gone) {
			t.Errorf("the chat screen draws %s", gone)
		}
	}
	if i, menu := strings.Index(view, `aria-label="Categories"`), strings.Index(view, `class="more-body"`); i < menu || menu < 0 {
		t.Errorf("the categories are at %d, before the menu at %d, or the menu is missing: a categories row outside the menu", i, menu)
	}
	version := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")).Properties.(map[string]any)["version"].(string)
	if version == "" || strings.Count(view, "<script") != 1 || !strings.Contains(view, chatScript) {
		t.Errorf("the chat screen lacks its one script, or the day has no version %q", version)
	}
	if want := `<div class="chat-screen" id="conversation" data-day="` + fixedToday + `" data-version="` + version + `">`; !strings.Contains(view, want) {
		t.Errorf("the chat screen lacks %s", want)
	}
	// The composer is the plain chat-form: no Preview in it (the body editor's form in the menu keeps its own), and it
	// returns to the screen.
	composer := `<form method="POST" action="/days/` + fixedToday + `/say" class="chat-form">`
	if i := strings.Index(view, composer); i < 0 {
		t.Errorf("the chat screen lacks the composer %s", composer)
	} else if form := view[i:]; strings.Contains(form[:strings.Index(form, "</form>")], "Preview") ||
		!strings.Contains(form[:strings.Index(form, "</form>")], `<input type="hidden" name="back" value="/days/`+fixedToday+`/conversation">`) {
		t.Error("the composer offers Preview, or does not return to the screen")
	}
	if !strings.Contains(view, `<kbd>Enter</kbd> sends · <kbd>Shift</kbd>+<kbd>Enter</kbd> a new line`) {
		t.Error("the composer lacks its keyboard rule")
	}
	// The agent has answered last: the three dots are gone.
	conversationBody(t, c, "\n\n**lifelog** (09.40): Last night you slept 7 hours.\n")
	if strings.Contains(browse(t, h, "/days/"+fixedToday+"/conversation"), `class="thinking"`) {
		t.Error("the screen waits for an agent that has answered")
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
	if strings.Contains(view, `class="turn `) || strings.Contains(view, `aria-label="Facts"`) || strings.Contains(view, "<summary>Edit</summary>") {
		t.Error("a day with no page draws a turn, the margin of a page or the editor of one")
	}
}

// TestADayThatOffersNoSayIsToldNothingWasSaid: say is today's alone, so a past day has no composer and no invitation.
func TestADayThatOffersNoSayIsToldNothingWasSaid(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	view := browse(t, h, "/days/2020-01-01/conversation")
	if !strings.Contains(view, "Nothing was said on this day.") || strings.Contains(view, "Write to the agent") || strings.Contains(view, `name="text"`) {
		t.Errorf("the conversation of a past day invites a write, or lacks its empty state:\n%s", view)
	}
	if !strings.Contains(view, `id="conversation" data-day="2020-01-01" data-version=""`) {
		t.Error("the view of a day with no page lacks its empty data-version")
	}
}

// TestLoginDrawsNoScriptAndPagesOfTheShellDrawOne: the script rides on the shell, not on every page.
func TestLoginDrawsNoScriptAndPagesOfTheShellDrawOne(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	login, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	rec := request(login, "GET", "/login", "", map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<form") || strings.Contains(rec.Body.String(), "<script") {
		t.Errorf("GET /login: %d, want a form page with no script", rec.Code)
	}
	for _, p := range []string{"/browse", "/settings", "/days/today/conversation"} {
		if n := strings.Count(browse(t, h, p), `<script`); n != 1 {
			t.Errorf("%s has %d script elements, want 1", p, n)
		}
	}
}

// TestEveryPageOfTheShellDrawsTheChatColumn: the column holds today's turns, the three dots when the last turn is
// the owner's, a form of say to today whose back is the page, and the version of the conversation page; the script
// is loaded once, with defer. The composer posts, and the page comes back to where it was.
func TestEveryPageOfTheShellDrawsTheChatColumn(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	day := "/days/" + fixedToday
	empty := browse(t, h, day)
	for _, want := range []string{`<div class="chat compact" id="chat" popover role="complementary" data-day="today" data-version="" aria-label="Chat">`, "Nothing is said today yet",
		`<form method="POST" action="/days/today/say" class="chat-form">`, `<input type="hidden" name="back" value="` + day + `">`,
		`popovertarget="chat"`, chatScript} {
		if !strings.Contains(empty, want) {
			t.Errorf("the page of a day with no talk lacks %s", want)
		}
	}
	if strings.Count(empty, `<script`) != 1 || strings.Contains(empty, `class="thinking"`) || strings.Contains(chatColumn.FindString(empty), "Preview") {
		t.Error("the column draws more than one script, the three dots with no turn, or a Preview button")
	}

	rec := submit(h, "/days/today/say", url.Values{"text": {"Anyone there?"}, "back": {day}}, day)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), day+"?feedback=") {
		t.Fatalf("POST /days/today/say with back: %d to %q, want 303 to the page", rec.Code, rec.Header().Get("Location"))
	}
	conv := apitest.Must(t)(c.Get(t.Context(), "/days/today/conversation"))
	version := conv.Properties.(map[string]any)["version"].(string)
	page := browse(t, h, day)
	column := chatColumn.FindString(page)
	for _, want := range []string{`data-version="` + version + `"`, `<li class="turn owner">`, "Anyone there?", `<div class="thinking" role="status"`} {
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
	if !strings.Contains(rec.Body.String(), "application/vnd.siren+json") || !strings.Contains(rec.Body.String(), "/conversation") || !strings.Contains(rec.Body.String(), "dataset.day") {
		t.Error("chat.js does not poll the conversation of the day of its element")
	}
}

// cardsWorld makes the things that an agent touches, each by the API: a day with a mood, a page, a person, a metric
// with a reading, a plan line and a word to search. It returns the addresses, in the order of the tool lines that name
// them. Today is fixedToday, so the reading of the day before is "yesterday".
func cardsWorld(t *testing.T, c *client.Client) []string {
	t.Helper()
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "Saw a heron at the river", "mood": "4"}))
	river := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "River walk", "body": "A long path along the water.\nA heron lives there."}))
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Sam", "name": "Samuel Ortiz"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": "2031-05-12", "value": "70.4"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": "2031-05-13", "value": "71.2"}))
	plan := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "plan"), map[string]string{"page": "today", "text": "Call the bank", "on": "today"}))
	line, err := plan.Result.(map[string]any)["line"].(json.Number).Int64()
	if err != nil {
		t.Fatal(err)
	}
	return []string{"/days/" + fixedToday, apitest.Href(river, "self"), apitest.Href(sam, "self"), "/metrics/Weight",
		fmt.Sprintf("/days/%s#line-%d", fixedToday, line), "/search?q=heron"}
}

// toolLines are the tool lines that name each address, as an agent writes them.
func toolLines(refs ...string) string {
	var b strings.Builder
	for i, ref := range refs {
		fmt.Fprintf(&b, "- `tool%d` → [%s](%s)\n", i+1, ref, ref)
	}
	return b.String()
}

// cardsOf are the cards of a page, in the order they are drawn: the kind in the class, the address, and the whole card.
func cardsOf(page string) [][]string {
	return regexp.MustCompile(`(?s)<a class="card card-([a-z]+)" href="([^"]*)">(.*?)</a>`).FindAllStringSubmatch(page, -1)
}

// TestTheConversationDrawsACardForEachThingTheAgentTouched: the tool lines of an agent's turn name a day, a page, a
// person, a metric, a plan line and a search, each made by the API first. The turn draws one card each, in that
// order, each a link to the thing with the parts of its class. An address that answers 404 draws the plain link, and
// the seventh address of a turn draws no card.
func TestTheConversationDrawsACardForEachThingTheAgentTouched(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	refs := cardsWorld(t, c)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")), "say"), map[string]string{"text": "What did I touch?"}))
	seven := []string{"/pages/999999", "/days/2031-05-02", "/days/2031-05-03", "/days/2031-05-04", "/days/2031-05-05", "/days/2031-05-06", "/days/2031-05-07"}
	conversationBody(t, c, "\n\n"+toolLines(refs...)+"\n**lifelog** (09.30): Here they are.\n\n"+toolLines(seven...)+"\n**lifelog** (09.31): And a week.\n\n"+toolLines("/days/today")+"\n**lifelog** (09.32): And today, by the word.\n")

	page := browse(t, h, "/days/"+fixedToday+"/conversation")
	cards := cardsOf(page)
	var kinds, hrefs []string
	for _, card := range cards[:6] {
		kinds, hrefs = append(kinds, card[1]), append(hrefs, card[2])
	}
	if got, want := strings.Join(kinds, " "), "day page person series plan search"; got != want {
		t.Fatalf("the cards of the first turn are %s, want %s", got, want)
	}
	if got, want := strings.Join(hrefs, " "), strings.Join(refs, " "); got != want {
		t.Errorf("the cards link to %s, want %s", got, want)
	}
	for i, want := range [][]string{
		{`<span class="leaf-day">14</span>`, `<span class="leaf-month">May 2031</span>`, "mood 4", "Saw a heron at the river", "Wed 14 May"},
		{"River walk", "A long path along the water."},
		{"Sam"},
		{"Weight", "71.2 kg · yesterday", `<svg class="trend"`},
		{"Call the bank", "open", "on Wed 14 May"},
		{"Search: heron", "3 hits"}, // the day page, the page and the conversation itself, which says heron in its tool line
	} {
		for _, part := range want {
			if !strings.Contains(cards[i][3], part) {
				t.Errorf("the %s card lacks %q:\n%s", cards[i][1], part, cards[i][3])
			}
		}
	}
	// The second turn names a 404 and six days: the plain link and five cards, and the seventh address is not drawn.
	if cards[6][1] != "link" || cards[6][2] != "/pages/999999" || cards[6][3] != "/pages/999999" {
		t.Errorf("the card of a 404 is %q, want the plain link", cards[6][0])
	}
	if got := len(cards) - 7; got != 6 { // the first turn, this one, and the one of today by the word
		t.Errorf("the turn of seven addresses draws %d cards, want 6", got)
	}
	for _, card := range cards[6:12] {
		if card[2] == seven[6] {
			t.Error("the seventh address of a turn is drawn")
		}
	}
	// The third turn names today by the word: the redirect is followed, and the card is the day's.
	if len(cards) != 13 || cards[12][1] != "day" || cards[12][2] != "/days/today" || !strings.Contains(cards[12][3], `<span class="leaf-day">14</span>`) {
		t.Errorf("the card of /days/today: %d cards, the last %v; want the day card of the 14th", len(cards), cards[len(cards)-1][:3])
	}
}

// TestTheColumnDrawsTheCompactCards: the column of any page draws the newest cards in the class compact, as
// the same markup; only the stylesheet makes them one line.
func TestTheColumnDrawsTheCompactCards(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	refs := cardsWorld(t, c)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")), "say"), map[string]string{"text": "Show me"}))
	conversationBody(t, c, "\n\n"+toolLines(refs[0], refs[3], "/pages/999999")+"\n**lifelog** (09.30): Here.\n")
	column := chatColumn.FindString(browse(t, h, "/browse"))
	cards := cardsOf(column)
	if len(cards) != 3 || cards[0][1] != "day" || cards[1][1] != "series" || cards[2][1] != "link" {
		t.Fatalf("the column draws the cards %v, want a day, a series and a plain link:\n%s", cards, column)
	}
	for _, want := range []string{`class="chat compact"`, "Weight", "71.2 kg · yesterday", "mood 4"} {
		if !strings.Contains(column, want) {
			t.Errorf("the column lacks %q", want)
		}
	}
}

// TestACardAsksForWhatThePageMayRead: the card is read with a source of its own, agent:card, which the owner-only
// reads refuse. A tool line that names /settings or /integrity draws the plain link on the owner's page and on an
// agent's page alike, and the integrity check does not run. On a public listener the cards follow the guard of the
// page: the cookie opens the page and its cards.
func TestACardAsksForWhatThePageMayRead(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday+"/conversation")), "say"), map[string]string{"text": "Settings?"}))
	conversationBody(t, c, "\n\n"+toolLines("/settings", "/integrity")+"\n**lifelog** (09.30): No.\n")
	// The card's source is refused the owner-only reads by the API itself.
	for _, path := range []string{"/settings", "/integrity"} {
		rec := request(h, "GET", path, "", map[string]string{"Accept": "application/vnd.siren+json", siren.SourceHeader: "agent:card"})
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as agent:card: %d, want 403", path, rec.Code)
		}
	}
	owner := browse(t, h, "/days/"+fixedToday+"/conversation")
	req := httptest.NewRequest("GET", "/days/"+fixedToday+"/conversation", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set(siren.SourceHeader, "agent:x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for who, page := range map[string]string{"the owner's": owner, "an agent's": rec.Body.String()} {
		got := cardsOf(page)
		if len(got) != 2 || got[0][1] != "link" || got[0][3] != "/settings" || got[1][1] != "link" || got[1][3] != "/integrity" {
			t.Errorf("%s page draws %v, want the plain links of /settings and /integrity", who, got)
		}
		if strings.Contains(page, "model_key") || strings.Contains(strings.ToLower(page), "card-integrity") {
			t.Errorf("%s page draws an owner-only read", who)
		}
	}

	public, s := publicHandler(t, api.PublicOptions{Token: testToken})
	today := s.Today()
	if _, _, err := s.CreatePage(t.Context(), "cli", "Conversation "+today, "[["+today+"]]\n\n- `open_day` → [/days/"+today+"](/days/"+today+")\n\n**lifelog** (09.00): Here.\n", ""); err != nil {
		t.Fatal(err)
	}
	with := map[string]string{"Accept": "text/html", "Cookie": api.CookieName + "=" + testToken}
	rec = request(public, "GET", "/days/today/conversation", "", with)
	if got := cardsOf(rec.Body.String()); rec.Code != 200 || len(got) != 1 || got[0][1] != "day" {
		t.Errorf("the public listener with the cookie: %d, cards %v, want a day card", rec.Code, got)
	}
	if rec = request(public, "GET", "/days/today/conversation", "", map[string]string{"Accept": "text/html"}); rec.Code != 303 || strings.Contains(rec.Body.String(), "card") {
		t.Errorf("the public listener with no cookie: %d, want the login redirect", rec.Code)
	}
}
