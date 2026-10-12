// app.js — this application's one script, written into every page like the two sheets. It makes a page better,
// and no page needs it: every form posts and every link works without it
// (docs/plans/091-the-browser-for-daily-use.md). It reads /titles (the names after [[) and /render (the preview of
// a body), and nothing else; it writes nothing. Five things:
//   1. after [[ in a textarea, the names of the pages that start with what follows;
//   2. a preview of a body as it will show when saved;
//   3. keys: Ctrl+K search from anywhere, Ctrl+Enter sends the form of a textarea, Escape leaves a field; outside a
//      field, / search, c capture, t today, [ and ] the previous and next page, Escape closes the toast;
//   4. a question before the browser leaves text that was not sent;
//   5. the design system's small things (docs/plans/092-the-app-shell.md): the toast's Close, a second press for a
//      danger button, and the current section in view in the sidebar.
(function () {
  "use strict";

  var siren = { Accept: "application/vnd.siren+json" };

  // ---- 1. Suggestions after [[

  // openLink is the open [[ before the caret: where its title starts and what is typed of it; null when the caret
  // is not inside an open link (a ], a | or a line break closes it, as in the wikilink grammar).
  function openLink(area) {
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

  // choose writes the title chosen and closes the link, unless the text after the caret already closes it.
  function choose(open, i) {
    var area = open.area, title = open.titles[i].title, end = area.selectionStart;
    var after = area.value.slice(end);
    var close2 = after.slice(0, 2) === "]]" ? "" : "]]";
    area.setRangeText(title + close2, open.link.start, end, "end");
    if (!close2) area.setSelectionRange(open.link.start + title.length + 2, open.link.start + title.length + 2);
    close(area);
    area.dispatchEvent(new Event("input", { bubbles: true }));
    area.focus();
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
    area.insertAdjacentElement("afterend", list);
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
    // Ctrl+K (Cmd+K) goes to the search from anywhere, a field too.
    if ((ev.ctrlKey || ev.metaKey) && !ev.altKey && !ev.shiftKey && (ev.key === "k" || ev.key === "K")) {
      var box = document.querySelector("form[role=search] input");
      if (box) { ev.preventDefault(); box.focus(); box.select(); }
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
        var search = document.querySelector("form[role=search] input");
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
      case "Escape": {
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

  // ---- 5. The toast, the danger button and the sidebar

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

  // ---- Start

  var areas = document.querySelectorAll("textarea:not([readonly])");
  for (var i = 0; i < areas.length; i++) {
    var area = areas[i];
    area.setAttribute("aria-autocomplete", "list");
    area.addEventListener("input", function (ev) { suggest(ev.target); });
    area.addEventListener("keydown", listKeys);
    area.addEventListener("blur", function (ev) { var a = ev.target; setTimeout(function () { close(a); }, 150); });
    if (area.name === "body") preview(area);
  }
  var toasts = document.querySelectorAll(".toast");
  for (var k = 0; k < toasts.length; k++) closable(toasts[k]);
  var search = document.querySelector(".rail-search");
  if (search) {
    var hint = document.createElement("kbd");
    hint.textContent = "Ctrl K";
    search.appendChild(hint);
  }
  // On a phone the sections are one row that scrolls to the side: the current one is brought into it.
  var here = document.querySelector(".rail nav a[aria-current=page]");
  if (here && here.scrollIntoView) here.scrollIntoView({ block: "nearest", inline: "nearest" });
  var foot = document.querySelector("footer p");
  if (foot) {
    var keys = document.createElement("small");
    keys.className = "keys";
    keys.textContent = "Keys: Ctrl+K or / search · c capture · t today · [ ] previous, next · Esc leaves a field · Ctrl+Enter sends a text";
    foot.appendChild(document.createElement("br"));
    foot.appendChild(keys);
  }
})();
