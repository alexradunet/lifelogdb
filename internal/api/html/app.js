// app.js — this application's one script, written into every page like the two sheets. It makes a page better,
// and no page needs it: every form posts and every link works without it
// (docs/plans/091-the-browser-for-daily-use.md). It reads /titles (the names after [[, and the pages of the command
// list), /render (the preview of a body), / (the root's actions, for the command list) and the chat pages
// (/conversations/{id}, for the sidebar's thread); the one write it sends itself is the sidebar's question to
// POST /conversations, the form the server drew. Seven things:
//   1. after [[ in a textarea, and in a field for a page's title (data-pages), the names of the pages that start
//      with what is typed;
//   2. a preview of a body as it will show when saved;
//   3. keys: Ctrl+K the command list from anywhere, Ctrl+Enter sends the form of a textarea, Escape leaves a
//      field; outside a field, / search, c capture, t today, [ and ] the previous and next page, ? the command list
//      with its keys, Escape closes the phone's sheet of sections or the toast;
//   4. a question before the browser leaves text that was not sent;
//   5. the design system's small things (docs/plans/092-the-app-shell.md): the toast's Close, a second press for a
//      danger button, and the hint of Ctrl+K in the search;
//   6. the command list of Ctrl+K (docs/plans/094-the-command-list.md);
//   7. the conversation (docs/plans/103-the-conversation-on-the-screen.md): the sidebar that follows the owner from
//      the chat page, its thread and its question, and the question shown at once on the chat page.
(function () {
  "use strict";

  var siren = { Accept: "application/vnd.siren+json" };

  // ---- 1. Suggestions after [[, and in a field for a page's title

  // openLink is the open [[ before the caret: where its title starts and what is typed of it; null when the caret
  // is not inside an open link (a ], a | or a line break closes it, as in the wikilink grammar). In a field for a
  // page's title the whole value is the title: whole says so, and no brackets are written.
  function openLink(area) {
    if (area.hasAttribute("data-pages")) return area.value ? { start: 0, typed: area.value, whole: true } : null;
    var before = area.value.slice(0, area.selectionStart);
    var at = before.lastIndexOf("[[");
    if (at < 0 || area.selectionStart !== area.selectionEnd) return null;
    var typed = before.slice(at + 2);
    if (/[\[\]|\r\n]/.test(typed) || typed.length > 120) return null;
    return { start: at + 2, typed: typed };
  }

  var lists = new WeakMap(); // a textarea → its open list of names

  function close(area) {
    var open = lists.get(area);
    if (!open) return;
    if (open.off) open.off();
    if (typeof open.list.hidePopover === "function" && open.list.matches(":popover-open")) open.list.hidePopover();
    open.list.remove();
    lists.delete(area);
    area.removeAttribute("aria-controls");
    area.setAttribute("aria-expanded", "false");
  }

  function select(open, i) {
    var items = open.list.children;
    if (!items.length) return;
    open.index = (i + items.length) % items.length;
    for (var k = 0; k < items.length; k++) items[k].setAttribute("aria-selected", k === open.index ? "true" : "false");
    open.area.setAttribute("aria-activedescendant", items[open.index].id);
    items[open.index].scrollIntoView({ block: "nearest" });
  }

  // choose writes the title chosen and closes the link, unless the text after the caret already closes it. In a
  // field for a page's title it is the value, and the list stays closed: the title is chosen.
  function choose(open, i) {
    var area = open.area, title = open.titles[i].title, end = area.selectionStart;
    if (open.link.whole) {
      area.value = title;
      close(area);
      area.focus();
      return;
    }
    var after = area.value.slice(end);
    var close2 = after.slice(0, 2) === "]]" ? "" : "]]";
    area.setRangeText(title + close2, open.link.start, end, "end");
    if (!close2) area.setSelectionRange(open.link.start + title.length + 2, open.link.start + title.length + 2);
    close(area);
    area.dispatchEvent(new Event("input", { bubbles: true }));
    area.focus();
  }

  // place puts a list that is a popover under its field: a popover is in the top layer, so it is fixed to the window.
  function place(open) {
    var r = open.area.getBoundingClientRect();
    open.list.style.left = r.left + "px";
    open.list.style.top = (r.bottom + 2) + "px";
    open.list.style.width = r.width + "px";
  }

  var serial = 0;

  function show(area, link, titles) {
    close(area);
    if (!titles.length) return;
    var list = document.createElement("ul");
    list.className = "suggest";
    list.id = "suggest-" + (++serial);
    list.setAttribute("role", "listbox");
    list.setAttribute("aria-label", "Pages");
    var open = { area: area, link: link, titles: titles, list: list, index: 0 };
    titles.forEach(function (t, i) {
      var li = document.createElement("li");
      li.id = list.id + "-" + i;
      li.setAttribute("role", "option");
      li.textContent = t.title;
      if (t.name !== t.title) {
        var small = document.createElement("small");
        small.textContent = " (" + t.name + ")";
        li.appendChild(small);
      }
      li.addEventListener("mousedown", function (ev) { ev.preventDefault(); choose(open, i); });
      list.appendChild(li);
    });
    var popover = typeof list.showPopover === "function"; // a browser without the API keeps the list in the page
    if (popover) list.setAttribute("popover", "manual");
    area.insertAdjacentElement("afterend", list);
    if (popover) {
      list.showPopover();
      place(open);
      // The list is fixed to the window, so a scroll or a resize of the page closes it. The list's own scroll does not.
      var away = function (ev) { if (!(ev.target instanceof Node) || !list.contains(ev.target)) close(area); };
      window.addEventListener("scroll", away, true);
      window.addEventListener("resize", away);
      open.off = function () { window.removeEventListener("scroll", away, true); window.removeEventListener("resize", away); };
    }
    area.setAttribute("aria-controls", list.id);
    area.setAttribute("aria-expanded", "true");
    lists.set(area, open);
    select(open, 0);
  }

  var asked = 0, wait = 0;

  function suggest(area) {
    clearTimeout(wait);
    var link = openLink(area);
    var prefix = link && link.typed.trim();
    if (!prefix) { close(area); return; }
    wait = setTimeout(function () {
      var mine = ++asked;
      fetch("/titles?" + new URLSearchParams({ prefix: prefix, limit: "8" }), { headers: siren, credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (e) {
          var now = openLink(area);
          if (mine !== asked || !now || now.start !== link.start) return;
          show(area, now, (e && e.properties && e.properties.titles) || []);
        })
        .catch(function () { close(area); });
    }, 120);
  }

  function listKeys(ev) {
    var open = lists.get(ev.target);
    if (!open) return;
    switch (ev.key) {
      case "ArrowDown": ev.preventDefault(); select(open, open.index + 1); break;
      case "ArrowUp": ev.preventDefault(); select(open, open.index - 1); break;
      case "Enter":
      case "Tab":
        if (ev.ctrlKey || ev.metaKey || ev.shiftKey || ev.altKey) return;
        ev.preventDefault(); ev.stopPropagation(); choose(open, open.index); break;
      case "Escape": ev.preventDefault(); close(ev.target); break;
    }
  }

  // ---- 2. The preview of a body

  // preview puts a body's textarea beside (on a wide screen) or above a panel that shows the body as it will show
  // when saved: the server renders it, 400 ms after the typing stops.
  function preview(area) {
    var label = area.closest("label");
    if (!label) return;
    var editor = document.createElement("div");
    editor.className = "editor";
    var box = document.createElement("div");
    box.className = "preview prose";
    box.setAttribute("aria-live", "polite");
    box.setAttribute("aria-label", "Preview");
    box.innerHTML = "<p><small>The preview shows here as you type.</small></p>";
    label.insertAdjacentElement("beforebegin", editor);
    editor.appendChild(label);
    editor.appendChild(box);
    var timer = 0, last = null;
    function run() {
      var body = area.value;
      if (body === last) return;
      last = body;
      fetch("/render", {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/x-www-form-urlencoded", Accept: "text/html" },
        body: new URLSearchParams({ body: body })
      })
        .then(function (r) { return r.ok ? r.text() : Promise.reject(r.status); })
        .then(function (html) {
          // The server renders with the safe defaults of every saved body: raw HTML is omitted.
          if (area.value === body) box.innerHTML = html || "<p><small>Nothing to show.</small></p>";
        })
        .catch(function () { last = null; box.textContent = "The preview is not available now."; });
    }
    area.addEventListener("input", function () { clearTimeout(timer); timer = setTimeout(run, 400); });
  }

  // ---- 3. Keys

  function inField(el) {
    return el && el.closest && el.closest("input, textarea, select, [contenteditable]") !== null;
  }

  document.addEventListener("keydown", function (ev) {
    var t = ev.target;
    if ((ev.ctrlKey || ev.metaKey) && ev.key === "Enter" && t instanceof HTMLTextAreaElement && t.form && !t.readOnly) {
      ev.preventDefault();
      t.form.requestSubmit();
      return;
    }
    // Ctrl+K (Cmd+K) opens the command list from anywhere, a field too.
    if ((ev.ctrlKey || ev.metaKey) && !ev.altKey && !ev.shiftKey && (ev.key === "k" || ev.key === "K")) {
      ev.preventDefault();
      openPalette();
      return;
    }
    // Escape leaves a field (a page may start in one), so the keys below work; it first closes a list of names.
    if (ev.key === "Escape" && !ev.defaultPrevented && inField(t) && !lists.get(t)) {
      t.blur();
      return;
    }
    if (ev.ctrlKey || ev.metaKey || ev.altKey || ev.defaultPrevented || inField(t)) return;
    var follow = function (selector) {
      var a = document.querySelector(selector);
      if (a) { ev.preventDefault(); a.click(); }
    };
    switch (ev.key) {
      case "/": {
        var search = document.querySelector("search input[type=search]");
        if (search) { ev.preventDefault(); search.focus(); }
        break;
      }
      case "c": {
        ev.preventDefault();
        var capture = document.querySelector('form[action$="/capture"] textarea[name=text]');
        if (capture) capture.focus(); else location.href = "/";
        break;
      }
      case "t": ev.preventDefault(); location.href = "/days/today"; break;
      case "?": ev.preventDefault(); openPalette(); break; // the command list shows every key in its last line
      case "Escape": {
        if (location.hash === "#sections") { ev.preventDefault(); location.replace("#closed"); break; } // the phone's sheet
        var toast = document.querySelector(".toast");
        if (toast) { ev.preventDefault(); toast.remove(); }
        break;
      }
      case "[": follow("main a[rel=prev]"); break;
      case "]": follow("main a[rel=next]"); break;
    }
  });

  // ---- 4. Text that was not sent

  var sent = null; // the form being sent: its own text is not lost
  document.addEventListener("submit", function (ev) { sent = ev.target; }, true);
  window.addEventListener("beforeunload", function (ev) {
    if (chatPending) { // the answer runs in its request: leaving the page stops it
      ev.preventDefault();
      ev.returnValue = "";
      return;
    }
    var areas = document.querySelectorAll("textarea");
    for (var i = 0; i < areas.length; i++) {
      var a = areas[i];
      if (!a.readOnly && a.value !== a.defaultValue && a.form !== sent) {
        ev.preventDefault();
        ev.returnValue = "";
        return;
      }
    }
  });

  // ---- 5. The toast and the danger button

  // closable gives a toast its Close. A toast stays until it is closed or the page changes: it may name what a write
  // did not do (names that made no link).
  function closable(toast) {
    var b = document.createElement("button");
    b.type = "button";
    b.className = "close";
    b.textContent = "Close";
    b.addEventListener("click", function () { toast.remove(); });
    toast.appendChild(b);
  }

  // A danger button (the catalog's danger: the action hides or retracts what is there) asks once, in the page: the
  // first press changes its word, the second sends the form. After 4 seconds it asks again.
  document.addEventListener("click", function (ev) {
    var b = ev.target.closest && ev.target.closest("button.danger");
    if (!b || !b.form || b.dataset.armed === "1") return;
    ev.preventDefault();
    var word = b.textContent;
    b.dataset.armed = "1";
    b.textContent = "Press again to " + word.toLowerCase();
    setTimeout(function () { b.dataset.armed = ""; b.textContent = word; }, 4000);
  });

  // ---- 6. The command list (docs/plans/094-the-command-list.md)

  // The list finds what is typed among a day, this page's actions, the sidebar's sections, the pages (by the start of
  // their title, from /titles), the root's actions (from /, once) and a search of the bodies. It never builds a form:
  // an action opens the form the server drew for it, here or on Home (/#do-NAME).

  function fold(s) { return s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase(); }

  // rank is 0 when a word of the name starts with the query, 1 when the name holds it, -1 when it does not.
  function rank(name, query) {
    var n = fold(name), q = fold(query.trim());
    if (!q) return 0;
    if (n.indexOf(q) === 0 || n.indexOf(" " + q) >= 0) return 0;
    return n.indexOf(q) >= 0 ? 1 : -1;
  }

  // pageActions are the actions of this page that the owner fills or presses: a box's, or the capture's; not a row's
  // one-button forms (Done, Skip), which belong to their row.
  function pageActions() {
    var out = [], seen = {};
    var forms = document.querySelectorAll("main form[data-action]");
    for (var i = 0; i < forms.length; i++) {
      var f = forms[i], name = f.getAttribute("data-action"), box = f.closest("details.action");
      if (seen[name] || (!box && !f.querySelector("label, fieldset"))) continue;
      seen[name] = true;
      var summary = box && box.querySelector("summary");
      var label = summary ? summary.firstChild.textContent : (f.querySelector("button") || {}).textContent || name;
      var capture = !box && f.closest(".capture");
      if (capture && capture.querySelector("h2")) label = capture.querySelector("h2").textContent;
      out.push({ kind: summary && summary.querySelector(".owner") ? "owner, on this page" : "on this page", name: label.trim(), action: name });
    }
    return out;
  }

  function sections() {
    var out = [], links = document.querySelectorAll(".rail nav a[href]:not([href^=\"#\"])");
    for (var i = 0; i < links.length; i++) {
      var name = links[i].firstChild && links[i].textContent.replace(/\d+$/, "").trim();
      out.push({ kind: "section", name: name, href: links[i].getAttribute("href") });
    }
    return out;
  }

  var rootActions = null; // the root's actions, read once a page
  function readRoot() {
    if (rootActions) return Promise.resolve(rootActions);
    return fetch("/", { headers: siren, credentials: "same-origin" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (e) {
        var count = {}, out = [];
        var actions = (e && e.actions) || [];
        actions.forEach(function (a) { count[a.name] = (count[a.name] || 0) + 1; });
        actions.forEach(function (a) {
          // An action offered once starts something. One offered per item (a check-in, an outcome) belongs to its
          // item, and the search is the list's own last row.
          if (count[a.name] === 1 && a.name !== "search") out.push({ kind: a.owner ? "owner" : "action", name: a.title || a.name, action: a.name, root: true });
        });
        rootActions = out;
        return out;
      })
      .catch(function () { return []; });
  }

  // openAction opens the form of an action on this page: the boxes around it, then the cursor in its first field.
  function openAction(name) {
    var f = document.querySelector("main form[data-action=\"" + CSS.escape(name) + "\"]");
    if (!f) return false;
    for (var d = f.closest("details"); d; d = d.parentElement && d.parentElement.closest("details")) d.open = true;
    var field = f.querySelector("input:not([type=hidden]), textarea, select, button");
    f.scrollIntoView({ block: "center" });
    if (field) field.focus();
    return true;
  }

  var palette = null, listed = [], chosen = 0, typed = 0, before = null;

  function build() {
    palette = document.createElement("dialog");
    palette.className = "palette";
    palette.setAttribute("aria-label", "Go to, or do");
    var input = document.createElement("input");
    input.type = "text";
    input.autocomplete = "off";
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-expanded", "true");
    input.setAttribute("aria-controls", "palette-list");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-label", "Go to a page, a section or a day, or do an action");
    input.placeholder = "Go to a page, a section or a day, or do an action";
    var list = document.createElement("ul");
    list.id = "palette-list";
    list.setAttribute("role", "listbox");
    list.setAttribute("aria-label", "Results");
    var keys = document.createElement("p");
    keys.className = "meta";
    // Every key the script takes, each in a kbd.
    [[["↑", "↓"], "move"], [["Enter"], "opens"], [["Escape"], "closes, or leaves a field"], [["Ctrl K"], "the command list"],
      [["?"], "the same list"], [["/"], "search"], [["c"], "capture"], [["t"], "today"], [["["], "previous page"],
      [["]"], "next page"], [["Ctrl Enter"], "sends a text"]].forEach(function (item, n) {
      if (n) keys.appendChild(document.createTextNode(" · "));
      item[0].forEach(function (name, k) {
        if (k) keys.appendChild(document.createTextNode(" "));
        var kbd = document.createElement("kbd");
        kbd.textContent = name;
        keys.appendChild(kbd);
      });
      keys.appendChild(document.createTextNode(" " + item[1]));
    });
    palette.appendChild(input);
    palette.appendChild(list);
    palette.appendChild(keys);
    document.body.appendChild(palette);
    input.addEventListener("input", function () { fill(input.value); });
    input.addEventListener("keydown", function (ev) {
      switch (ev.key) {
        case "ArrowDown": ev.preventDefault(); markRow(chosen + 1); break;
        case "ArrowUp": ev.preventDefault(); markRow(chosen - 1); break;
        case "Enter": ev.preventDefault(); ev.stopPropagation(); go(listed[chosen]); break;
        case "Escape": ev.preventDefault(); ev.stopPropagation(); closePalette(); break;
      }
    });
    palette.addEventListener("click", function (ev) { if (ev.target === palette) closePalette(); }); // the backdrop
    palette.addEventListener("cancel", function (ev) { ev.preventDefault(); closePalette(); });
  }

  function openPalette() {
    if (!palette) build();
    if (!palette.open) {
      before = document.activeElement;
      palette.showModal();
    }
    var input = palette.querySelector("input");
    input.value = "";
    input.focus();
    fill("");
  }

  function closePalette() {
    if (!palette || !palette.open) return;
    palette.close();
    if (before && before.focus) before.focus();
  }

  function fill(query) {
    var mine = ++typed, q = query.trim();
    var near = function (items) {
      return items.map(function (it) { return { it: it, r: rank(it.name, q) }; })
        .filter(function (x) { return x.r >= 0; })
        .sort(function (a, b) { return a.r - b.r; })
        .map(function (x) { return x.it; });
    };
    var rows = /^\d{4}-\d{2}-\d{2}$/.test(q) ? [{ kind: "day", name: "Open " + q, href: "/days/" + q }] : [];
    rows = rows.concat(near(pageActions()), near(sections()));
    var search = q ? [{ kind: "search", name: "Search the bodies for “" + q + "”", href: "/search?q=" + encodeURIComponent(q) }] : [];
    showRows(rows.concat(search));
    var pages = !q ? Promise.resolve([]) : fetch("/titles?" + new URLSearchParams({ prefix: q, limit: "6" }), { headers: siren, credentials: "same-origin" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (e) {
        return ((e && e.properties && e.properties.titles) || []).map(function (t) {
          return { kind: "page", name: t.title, href: "/pages?title=" + encodeURIComponent(t.title) };
        });
      })
      .catch(function () { return []; });
    Promise.all([pages, readRoot()]).then(function (got) {
      if (mine !== typed) return; // a later key has its own rows
      var here = {};
      pageActions().forEach(function (a) { here[a.action] = true; });
      showRows(rows.concat(got[0], near(got[1].filter(function (a) { return !here[a.action]; })), search));
    });
  }

  function showRows(rows) {
    listed = rows.slice(0, 12);
    var list = palette.querySelector("ul");
    list.textContent = "";
    listed.forEach(function (row, i) {
      var li = document.createElement("li");
      li.id = "palette-" + i;
      li.setAttribute("role", "option");
      var kind = document.createElement("span");
      kind.className = "kind";
      kind.textContent = row.kind;
      var name = document.createElement("span");
      name.textContent = row.name;
      li.appendChild(name);
      li.appendChild(kind);
      li.addEventListener("mousedown", function (ev) { ev.preventDefault(); go(row); });
      list.appendChild(li);
    });
    if (!listed.length) {
      var none = document.createElement("li");
      none.className = "none";
      none.textContent = "Nothing matches. Type the start of a title, a section or an action.";
      list.appendChild(none);
    }
    markRow(0);
  }

  function markRow(i) {
    var items = palette.querySelectorAll("li[role=option]"), input = palette.querySelector("input");
    if (!items.length) { input.removeAttribute("aria-activedescendant"); return; }
    chosen = (i + items.length) % items.length;
    for (var k = 0; k < items.length; k++) items[k].setAttribute("aria-selected", k === chosen ? "true" : "false");
    input.setAttribute("aria-activedescendant", items[chosen].id);
    items[chosen].scrollIntoView({ block: "nearest" });
  }

  function go(row) {
    if (!row) return;
    if (row.action && !row.root) { // an action of this page: its own form, here
      before = null;
      closePalette();
      openAction(row.action);
      return;
    }
    location.href = row.action ? "/#do-" + encodeURIComponent(row.action) : row.href;
  }

  // ---- 7. The conversation (docs/plans/103-the-conversation-on-the-screen.md)

  // The state of the sidebar lives in this browser: open or closed, and the current conversation. The chat page sets
  // it open, so the page the owner goes to next shows the same conversation in the sidebar.
  var chatKey = "lifelog.chat";
  var chatPending = false; // a question waits for its answer
  function chatState() { try { return JSON.parse(localStorage.getItem(chatKey)) || {}; } catch (e) { return {}; } }
  function saveChat(s) { try { localStorage.setItem(chatKey, JSON.stringify(s)); } catch (e) { /* private mode */ } }

  // chatHTML reads a chat page's HTML: its thread (the server draws every turn), its conversation and its alert.
  function chatHTML(html) {
    var doc = new DOMParser().parseFromString(html, "text/html");
    var page = doc.querySelector("[data-chat-page]");
    return { thread: doc.querySelector(".chat-page .chat-thread"), id: page ? page.getAttribute("data-chat-page") : null,
      alert: doc.querySelector("[role=alert]") };
  }

  // turnWaiting draws the question as sent and "Thinking" under it, until the answer replaces the thread.
  function turnWaiting(thread, question) {
    var empty = thread.querySelector(".chat-empty");
    if (empty) empty.remove();
    var mine = document.createElement("div");
    mine.className = "msg owner";
    mine.innerHTML = '<div class="bubble"></div>';
    mine.firstChild.textContent = question;
    var wait = document.createElement("div");
    wait.className = "msg lifelog pending";
    wait.innerHTML = '<div class="bubble">Thinking… the answer can take a minute.</div>';
    thread.appendChild(mine);
    thread.appendChild(wait);
    return wait;
  }

  function chat() {
    var page = document.querySelector("[data-chat-page]");
    if (page) {
      saveChat({ open: true, id: page.getAttribute("data-chat-page") });
      var full = page.querySelector(".chat-composer form");
      if (full) full.addEventListener("submit", function () {
        var q = full.querySelector("textarea[name=question]");
        if (!q || !q.value.trim()) return;
        turnWaiting(page.querySelector(".chat-thread"), q.value.trim());
        full.querySelector("button").disabled = true;
        window.scrollTo(0, document.body.scrollHeight);
      });
      window.scrollTo(0, document.body.scrollHeight);
      return;
    }
    var panel = document.getElementById("chat-panel");
    var toggle = document.querySelector('[data-chat="toggle"]');
    if (!panel || !toggle) return;
    var shell = panel.parentElement, thread = panel.querySelector(".chat-thread"), form = panel.querySelector("form");
    var question = form.querySelector("textarea[name=question]"), send = form.querySelector("button");
    var blank = thread.innerHTML, loaded = null;

    function showThread(t) { thread.innerHTML = t ? t.innerHTML : blank; thread.scrollTop = thread.scrollHeight; }
    function current(id) {
      form.querySelector("input[name=conversation]").value = id || "";
      var links = panel.querySelectorAll('[data-chat="full"]');
      for (var i = 0; i < links.length; i++) links[i].href = id ? "/conversations/" + id : "/conversations";
      var s = chatState();
      s.id = id || "";
      saveChat(s);
    }
    function load(id) {
      loaded = id || "";
      current(id);
      if (!id) { showThread(null); return; }
      fetch("/conversations/" + encodeURIComponent(id), { headers: { Accept: "text/html" }, credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.text() : Promise.reject(r.status); })
        .then(function (html) { showThread(chatHTML(html).thread); })
        .catch(function () { load(""); }); // tombstoned, or gone: a new conversation
    }
    function setOpen(open) {
      panel.hidden = !open;
      shell.classList.toggle("chat-open", open);
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
      var s = chatState();
      s.open = open;
      saveChat(s);
      if (open && loaded === null) load(s.id);
    }

    toggle.hidden = false;
    toggle.addEventListener("click", function () { setOpen(true); question.focus(); });
    panel.querySelector('[data-chat="close"]').addEventListener("click", function () { setOpen(false); toggle.focus(); });
    panel.querySelector('[data-chat="new"]').addEventListener("click", function () { load(""); question.focus(); });
    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var q = question.value.trim();
      if (!q || chatPending) return;
      chatPending = true;
      send.disabled = true;
      var wait = turnWaiting(thread, q);
      thread.scrollTop = thread.scrollHeight;
      function failed(words) {
        wait.className = "msg lifelog failed";
        wait.firstChild.textContent = words;
      }
      fetch(form.action, { method: "POST", body: new URLSearchParams(new FormData(form)), credentials: "same-origin",
        headers: { Accept: "text/html" } })
        .then(function (r) { return r.text().then(function (html) { return { ok: r.ok, status: r.status, page: chatHTML(html) }; }); })
        .then(function (a) {
          if (a.ok && a.page.id) {
            loaded = a.page.id;
            current(a.page.id);
            showThread(a.page.thread);
            question.value = "";
          } else {
            failed("No answer. " + (a.page.alert ? a.page.alert.textContent.trim() : "HTTP " + a.status));
          }
        })
        .catch(function (e) { failed("No answer: " + e.message); })
        .then(function () { chatPending = false; send.disabled = false; });
    });
    if (chatState().open) setOpen(true);
  }

  // ---- Start

  var areas = document.querySelectorAll("textarea:not([readonly]), input[data-pages]");
  for (var i = 0; i < areas.length; i++) {
    var area = areas[i];
    area.setAttribute("aria-autocomplete", "list");
    if (area.hasAttribute("data-pages")) area.autocomplete = "off"; // the browser's own list would cover the names
    area.addEventListener("input", function (ev) { suggest(ev.target); });
    area.addEventListener("keydown", listKeys);
    area.addEventListener("blur", function (ev) { var a = ev.target; setTimeout(function () { close(a); }, 150); });
    if (area.name === "body") preview(area);
  }
  chat();
  var toasts = document.querySelectorAll(".toast");
  for (var k = 0; k < toasts.length; k++) closable(toasts[k]);
  var search = document.querySelector(".rail-search");
  if (search) {
    var hint = document.createElement("kbd");
    hint.textContent = "Ctrl K";
    hint.title = "Ctrl K opens the command list";
    search.appendChild(hint);
  }
  // On a phone the top bar's Search button opens the command list, which also searches the bodies.
  var searchButton = document.querySelector(".rail-search-link");
  if (searchButton) searchButton.addEventListener("click", function (ev) { ev.preventDefault(); openPalette(); });
  // Home opens the action that the command list chose on another page (/#do-NAME).
  if (location.hash.indexOf("#do-") === 0) openAction(decodeURIComponent(location.hash.slice(4)));
  var foot = document.querySelector("footer p");
  if (foot) {
    var keys = document.createElement("small");
    keys.className = "keys";
    keys.textContent = "Keys: Ctrl+K the command list · ? all the keys · / search · c capture · t today · [ ] previous, next · Esc leaves a field · Ctrl+Enter sends a text";
    foot.appendChild(document.createElement("br"));
    foot.appendChild(keys);
  }
})();
