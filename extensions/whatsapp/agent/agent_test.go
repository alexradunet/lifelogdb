package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const todayTitle = "Conversation 2031-05-20"

// userOf is the user message of a model request: the conversation page.
func userOf(r chatReq) string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			return r.Messages[i].Content
		}
	}
	return ""
}

func TestTheFirstTurnOfADayMakesThePageAndKeepsTheTurnOnIt(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("capture", `{"day":"2031-05-20","text":"Walked by the river."}`)
		}
		return text("Written to today.")
	})
	r.chat.say(t, "I walked by the river")
	if got := r.chat.reply(t); got != "Written to today." {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	want := "[[2031-05-20]]\n\n**Owner** (09.30): I walked by the river\n\n- `capture` → [/days/2031-05-20/capture](/days/2031-05-20/capture)\n\n**lifelog** (09.30): Written to today."
	if got := r.api.body(todayTitle); got != want {
		t.Errorf("page body\n%q\nwant\n%q", got, want)
	}
	if p := r.api.find(todayTitle); p.category != "Conversations" || p.version != 3 {
		t.Errorf("page %+v", p)
	}
	var says []seen
	for _, q := range r.api.requests() {
		if strings.HasSuffix(q.Path, "/say") {
			says = append(says, q)
		}
	}
	if len(says) != 1 || says[0].Path != "/days/today/say" || says[0].Source != "agent:whatsapp" || !strings.Contains(says[0].Body, "text=I+walked+by+the+river") {
		t.Errorf("say requests %+v", says)
	}
	w := r.api.writes()
	if len(w) != 1 || w[0].Path != "/days/2031-05-20/capture" || w[0].Source != "agent:whatsapp" || !strings.Contains(w[0].Body, "text=Walked+by+the+river.") {
		t.Errorf("writes %+v", w)
	}
	reqs := r.mod.requests()
	if len(reqs) != 2 || reqs[0].Model != "test-model" {
		t.Fatalf("model requests %+v", reqs)
	}
	if sys := reqs[0].Messages[0]; sys.Role != "system" || !strings.Contains(sys.Content, "Today is 2031-05-20") {
		t.Errorf("system message %+v", sys)
	}
	// The model sees the page with the owner's paragraph, which was saved before the model was called.
	if got := userOf(reqs[0]); got != "[[2031-05-20]]\n\n**Owner** (09.30): I walked by the river" || len(reqs[0].Messages) != 2 {
		t.Errorf("user message %q", got)
	}
	if last := reqs[1].Messages[len(reqs[1].Messages)-1]; last.Role != "tool" || last.ToolCallID != "call1" {
		t.Errorf("tool message %+v", last)
	}
	if got := r.chat.typing(); len(got) != 2 || !got[0] || got[1] {
		t.Errorf("typing %v, want on then off", got)
	}
	st, err := loadState(r.state)
	if err != nil || st.Day != "2031-05-20" || st.ID != "1" || st.Version != "3" || st.Body != want || st.SavedAt.IsZero() {
		t.Errorf("state %+v, %v", st, err)
	}
}

func TestTheSettingsAreReadAtEveryTurnWithTheOwnersTokenAndNoSource(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.chat.say(t, "one")
	r.chat.reply(t)
	r.api.setModel(r.api.modelURL, "other-model", "synthetic-key")
	r.chat.say(t, "two")
	r.chat.reply(t)
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	reqs := r.mod.requests()
	if len(reqs) != 2 || reqs[0].Model != "test-model" || reqs[1].Model != "other-model" {
		t.Errorf("model requests %+v, want the change between two turns to be seen", reqs)
	}
	reads, writes := 0, 0
	for _, q := range r.api.requests() {
		switch {
		case q.Path == "/settings":
			reads++
			if q.Method != "GET" || q.Source != "" {
				t.Errorf("settings request %+v, want a GET with no source", q)
			}
		case q.Method != "GET":
			writes++
			if q.Source != "agent:whatsapp" {
				t.Errorf("write %+v, want the source agent:whatsapp", q)
			}
		}
	}
	if reads != 2 || writes != 4 {
		t.Errorf("%d settings reads and %d writes, want 2 and 4", reads, writes)
	}
}

func TestATurnWithNoModelGivesOneLineOnThePageAndInTheChatAndTheAgentGoesOn(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text("answer") })
	url := r.api.modelURL
	r.api.setModel("", "", "")
	r.chat.say(t, "hello")
	const want = "No model is set: open Settings in lifelog."
	if got := r.chat.reply(t); got != want {
		t.Errorf("reply %q", got)
	}
	if body := r.api.body(todayTitle); !strings.HasSuffix(body, "**lifelog** (09.30): "+want) {
		t.Errorf("page body %q", body)
	}
	if n := len(r.mod.requests()); n != 0 {
		t.Errorf("%d model requests, want none", n)
	}
	r.api.setModel(url, "test-model", "")
	r.chat.say(t, "again")
	if got := r.chat.reply(t); got != "answer" {
		t.Errorf("reply after the model was set: %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestAnOwnerActionIsNeverATool(t *testing.T) {
	list, err := tools(catalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.name == "snapshot" {
			t.Error("the owner's action is a tool")
		}
	}
}

func TestAToolResultOverTheBoundIsCutWithANote(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("open_day", `{"day":"big"}`)
		}
		return text("read")
	})
	r.chat.say(t, "open it")
	r.chat.reply(t)
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	reqs := r.mod.requests()
	got := reqs[1].Messages[len(reqs[1].Messages)-1].Content
	if !strings.Contains(got, "[cut: the result has") || len(got) > maxResult+100 {
		t.Errorf("tool result has %d bytes and no note", len(got))
	}
}

func TestAnErrorOfTheAPIGoesToTheModelAsItsErrorEntityAndToThePageAsItsCode(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			return toolCall("get", `{"href":"/days/gone"}`)
		}
		return text("no such day")
	})
	r.chat.say(t, "x")
	r.chat.reply(t)
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	reqs := r.mod.requests()
	if got := reqs[1].Messages[len(reqs[1].Messages)-1].Content; !strings.Contains(got, `"code":"not_found"`) || !strings.Contains(got, `"status":404`) {
		t.Errorf("tool result %s", got)
	}
	if body := r.api.body(todayTitle); !strings.Contains(body, "- `get` failed: not_found") {
		t.Errorf("page body %q", body)
	}
}

func TestABadToolCallIsAnErrorForTheModelNotACrash(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		switch n {
		case 1:
			return toolCall("nope", `{}`)
		case 2:
			return toolCall("capture", `{"day":`)
		}
		return text("done")
	})
	r.chat.say(t, "x")
	if got := r.chat.reply(t); got != "done" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	reqs := r.mod.requests()
	if got := reqs[1].Messages[len(reqs[1].Messages)-1].Content; !strings.Contains(got, `no tool "nope"`) {
		t.Errorf("unknown tool result %q", got)
	}
	if got := reqs[2].Messages[len(reqs[2].Messages)-1].Content; !strings.HasPrefix(got, "error:") {
		t.Errorf("bad arguments result %q", got)
	}
	if len(r.api.writes()) != 0 {
		t.Error("a bad call wrote")
	}
}

func TestTheStepBoundEndsATurn(t *testing.T) {
	r := start(t, func(int, chatReq) (int, string) { return toolCall("open_day", `{"day":"2031-05-20"}`) })
	r.chat.say(t, "loop")
	if got := r.chat.reply(t); got != "I stopped after 16 steps." {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if n := len(r.mod.requests()); n != maxSteps {
		t.Errorf("%d model steps, want %d", n, maxSteps)
	}
	if body := r.api.body(todayTitle); strings.Count(body, "- `open_day`") != maxSteps || !strings.HasSuffix(body, "I stopped after 16 steps.") {
		t.Errorf("page body %q", body)
	}
}

func TestTheStepBoundGivesTheLastTextOfTheModel(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) {
		return 200, `{"choices":[{"message":{"content":"working on it","tool_calls":[{"id":"c","type":"function","function":{"name":"open_day","arguments":"{}"}}]}}]}`
	})
	r.chat.say(t, "loop")
	if got := r.chat.reply(t); got != "working on it" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestAModelErrorGivesTheFailedReplyOnThePageAndInTheChat(t *testing.T) {
	r := start(t, func(int, chatReq) (int, string) { return 503, "overloaded\nmore" })
	r.chat.say(t, "hi")
	got := r.chat.reply(t)
	if !strings.HasPrefix(got, "The model failed: ") || !strings.Contains(got, "503") || !strings.Contains(got, "overloaded") || strings.Contains(got, "more") || !strings.HasSuffix(got, ".") {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if body := r.api.body(todayTitle); !strings.HasSuffix(body, "**lifelog** (09.30): "+got) {
		t.Errorf("page body %q", body)
	}
}

func TestTwoMessagesAreAnsweredInOrderAndTheSecondSeesTheFirstOnThePage(t *testing.T) {
	r := start(t, func(n int, req chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.chat.say(t, "first")
	r.chat.say(t, "second")
	if a, b := r.chat.reply(t), r.chat.reply(t); a != "answer 1" || b != "answer 2" {
		t.Errorf("replies %q, %q", a, b)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	got := userOf(r.mod.requests()[1])
	want := "[[2031-05-20]]\n\n**Owner** (09.30): first\n\n**lifelog** (09.30): answer 1\n\n**Owner** (09.30): second"
	if got != want {
		t.Errorf("second user message\n%q\nwant\n%q", got, want)
	}
}

func TestAnotherSenderIsIgnoredAndAnImageGetsOneLine(t *testing.T) {
	r := start(t, func(int, chatReq) (int, string) { return text("hello") })
	r.chat.deliver(t, Message{Sender: "999", Text: "hi", Kind: KindText, At: fixedNow})
	r.chat.deliver(t, Message{Sender: "999", Kind: KindImage, At: fixedNow})
	r.chat.deliver(t, Message{Sender: owner, Kind: KindImage, At: fixedNow})
	if got := r.chat.reply(t); got != "I read text for now; send it as text." {
		t.Errorf("reply %q", got)
	}
	r.chat.say(t, "hi")
	if got := r.chat.reply(t); got != "hello" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1 (only the owner's text)", n)
	}
	if body := r.api.body(todayTitle); strings.Contains(body, "999") || strings.Contains(body, "image") {
		t.Errorf("page body %q", body)
	}
	select {
	case s := <-r.chat.sent:
		t.Errorf("an extra reply %q", s)
	default:
	}
}

func TestAFullQueueAnswersWithOneLine(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	r := start(t, func(n int, _ chatReq) (int, string) {
		if n == 1 {
			started <- struct{}{}
			<-release
		}
		return text("ok")
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	r.chat.say(t, "first")
	<-started
	for i := 0; i < queueSize; i++ {
		r.chat.say(t, fmt.Sprint("queued ", i))
	}
	r.chat.say(t, "one too many")
	if got := r.chat.reply(t); got != "still working on your earlier messages" {
		t.Errorf("reply %q", got)
	}
	close(release)
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if n := len(r.chat.sent); n != queueSize+1 {
		t.Errorf("%d replies after the notice, want %d", n, queueSize+1)
	}
}

func TestAStaleVersionReadsTheSaveAgainAndAppendsOnce(t *testing.T) {
	api := &fakeAPI{sneak: "Added on the web."}
	api.add(todayTitle, "[[2031-05-20]]") // the page exists, so the first save meets the sneak
	r := startOn(t, func(int, chatReq) (int, string) { return text("noted") }, api, t.TempDir()+"/state.json")
	r.chat.say(t, "hello")
	if got := r.chat.reply(t); got != "noted" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	body := api.body(todayTitle)
	if strings.Count(body, "**Owner** (09.30): hello") != 1 || !strings.Contains(body, "Added on the web.x") || !strings.HasSuffix(body, "**lifelog** (09.30): noted") {
		t.Errorf("page body %q", body)
	}
}

func TestASecondStaleVersionKeepsTheReplyOffThePageButSendsItToTheChat(t *testing.T) {
	api := &fakeAPI{alwaysStale: true}
	api.add(todayTitle, "[[2031-05-20]]")
	r := startOn(t, func(int, chatReq) (int, string) { return text("answer") }, api, t.TempDir()+"/state.json")
	r.chat.say(t, "hello")
	if got := r.chat.reply(t); got != "answer" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if body := api.body(todayTitle); strings.Contains(body, "**lifelog**") {
		t.Errorf("a reply that could not be saved is on the page: %q", body)
	}
}

func TestTheWatcherAnswersATurnThatWasWrittenOnAnotherSurface(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.chat.say(t, "from the phone")
	r.chat.reply(t)
	r.settle(t) // the extension's own save is not a change
	if n := len(r.mod.requests()); n != 1 {
		t.Fatalf("%d model requests after the own save, want 1", n)
	}
	r.api.edit(todayTitle, r.api.body(todayTitle)+"\n\nFrom the web: what did I do?")
	r.poll(t)
	if got := r.chat.reply(t); got != "answer 2" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	reqs := r.mod.requests()
	if got := userOf(reqs[1]); !strings.HasSuffix(got, "From the web: what did I do?") {
		t.Errorf("the model did not read the new paragraph: %q", got)
	}
	if body := r.api.body(todayTitle); strings.Count(body, "**Owner**") != 1 || !strings.HasSuffix(body, "**lifelog** (09.30): answer 2") {
		t.Errorf("page body %q", body)
	}
}

// TestTheWatcherReadsABodyThatABrowserSaved: a browser submits a textarea with CRLF line endings, and the API
// stores the body as sent. The read body is made LF, so it still has the saved body as its prefix, and the
// extension's next save writes LF.
func TestTheWatcherReadsABodyThatABrowserSaved(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text(fmt.Sprintf("answer %d", n)) })
	r.chat.say(t, "from the phone")
	r.chat.reply(t)
	r.settle(t)
	crlf := strings.ReplaceAll(r.api.body(todayTitle), "\n", "\r\n") + "\r\n\r\nFrom the browser: hello?"
	r.api.edit(todayTitle, crlf)
	r.poll(t)
	if got := r.chat.reply(t); got != "answer 2" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if body := r.api.body(todayTitle); strings.Contains(body, "\r") || !strings.HasSuffix(body, "From the browser: hello?\n\n**lifelog** (09.30): answer 2") {
		t.Errorf("page body %q", body)
	}
}

func TestTheWatcherAdoptsAnEditAndAnswersNothing(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text("answer") })
	r.chat.say(t, "hi")
	r.chat.reply(t)
	r.api.edit(todayTitle, "[[2031-05-20]]\n\nA word fixed.")
	r.settle(t)
	if n := len(r.mod.requests()); n != 1 {
		t.Errorf("%d model requests, want 1", n)
	}
	select {
	case s := <-r.chat.sent:
		t.Errorf("an edit was answered with %q", s)
	default:
	}
	// The edit is the base now: growth after it is a turn.
	r.api.edit(todayTitle, r.api.body(todayTitle)+"\n\nMore.")
	r.poll(t)
	if got := r.chat.reply(t); got != "answer" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestTheWatcherLooksTheTitleUpAgainOnANewDay(t *testing.T) {
	r := start(t, func(n int, _ chatReq) (int, string) { return text("answer") })
	r.chat.say(t, "today")
	r.chat.reply(t)
	next := fixedNow.AddDate(0, 0, 1)
	r.setDay(next)
	r.settle(t) // no page for the new day yet: nothing happens
	r.api.add("Conversation 2031-05-21", "[[2031-05-21]]")
	r.settle(t) // an unknown page is adopted, not answered
	if n := len(r.mod.requests()); n != 1 {
		t.Fatalf("%d model requests, want 1", n)
	}
	r.api.edit("Conversation 2031-05-21", "[[2031-05-21]]\n\nGood morning.")
	r.poll(t)
	if got := r.chat.reply(t); got != "answer" {
		t.Errorf("reply %q", got)
	}
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if body := r.api.body("Conversation 2031-05-21"); !strings.Contains(body, "Good morning.") || !strings.HasSuffix(body, "answer") {
		t.Errorf("page body %q", body)
	}
	if body := r.api.body(todayTitle); strings.Contains(body, "Good morning.") {
		t.Errorf("yesterday's page was written: %q", body)
	}
}

func TestARestartKeepsTheThreadAndDoesNotAnswerItsOwnSaves(t *testing.T) {
	api := &fakeAPI{}
	state := t.TempDir() + "/state.json"
	r := startOn(t, func(int, chatReq) (int, string) { return text("first answer") }, api, state)
	r.chat.say(t, "one")
	r.chat.reply(t)
	if err := r.finish(t); err != nil {
		t.Fatal(err)
	}
	if at, ok, err := LastSave(state); err != nil || !ok || at.IsZero() {
		t.Errorf("LastSave %v %v %v", at, ok, err)
	}
	r2 := startOn(t, func(int, chatReq) (int, string) { return text("second answer") }, api, state)
	r2.settle(t)
	r2.chat.say(t, "two")
	if got := r2.chat.reply(t); got != "second answer" {
		t.Errorf("reply %q", got)
	}
	if err := r2.finish(t); err != nil {
		t.Fatal(err)
	}
	if n := len(r2.mod.requests()); n != 1 {
		t.Errorf("%d model requests after the restart, want 1", n)
	}
	if got := userOf(r2.mod.requests()[0]); !strings.Contains(got, "**lifelog** (09.30): first answer") || !strings.HasSuffix(got, "**Owner** (09.30): two") {
		t.Errorf("the restarted agent lost the thread: %q", got)
	}
	if _, ok, err := LastSave(state + ".missing"); ok || err != nil {
		t.Errorf("LastSave of a missing file: %v %v", ok, err)
	}
}

func TestTheConversationSentToTheModelKeepsItsEnd(t *testing.T) {
	long := strings.Repeat("é", maxBody) // two bytes each
	got := tail(long + "END")
	if !strings.HasSuffix(got, "END") || len(got) > maxBody+100 || !strings.HasPrefix(got, "[the first ") {
		t.Errorf("tail has %d bytes, prefix %.30q", len(got), got)
	}
	if tail("short") != "short" {
		t.Error("a short conversation was changed")
	}
}

func TestACancelledContextEndsTheTurnAndTheWorker(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	r := start(t, func(n int, _ chatReq) (int, string) {
		started <- struct{}{}
		<-release
		return text("late")
	})
	t.Cleanup(func() { close(release) }) // runs before the server closes, so its handler can end
	r.chat.say(t, "hang")
	<-started
	r.cancel()
	select {
	case err := <-r.done:
		r.done <- err
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(wait):
		t.Fatal("Run did not return after the cancel")
	}
	select {
	case s := <-r.chat.sent:
		t.Errorf("a cancelled turn sent %q", s)
	default:
	}
	if body := r.api.body(todayTitle); strings.Contains(body, "**lifelog**") {
		t.Errorf("a cancelled turn wrote a reply: %q", body)
	}
}
