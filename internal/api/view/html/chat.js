// The one script of the app (README, "No script"). It only helps the chat column (#chat); every form works without it.
// It polls while the agent has not answered, posts the composer in place, and remembers whether the column is open.
// Any error leaves the column as it is.
(() => {
  const chat = document.getElementById('chat');
  if (!chat) return;
  const same = { credentials: 'same-origin' };
  const KEY = 'lifelog.chat.open';
  let timer;

  // swap replaces the column's content with the one in a page's HTML, and keeps a draft that is being typed.
  const swap = (html) => {
    const next = new DOMParser().parseFromString(html, 'text/html').getElementById('chat');
    if (!next) return;
    const draft = chat.querySelector('textarea');
    const typed = draft && !draft.form.dataset.sent ? draft.value : '';
    chat.dataset.version = next.dataset.version;
    chat.innerHTML = next.innerHTML;
    const box = chat.querySelector('textarea');
    if (box) box.value = typed;
    poll();
  };

  // poll asks for the page's version every 3 seconds while the last turn is the owner's.
  const poll = () => {
    clearTimeout(timer);
    if (!chat.querySelector('.waiting')) return;
    timer = setTimeout(async () => {
      try {
        const r = await fetch('/days/today/conversation', { ...same, headers: { Accept: 'application/vnd.siren+json' } });
        const version = ((await r.json()).properties || {}).version || '';
        if (version !== chat.dataset.version) {
          const page = await fetch(location.href, { ...same, headers: { Accept: 'text/html' } });
          if (page.ok) { swap(await page.text()); return; }
        }
      } catch (e) { /* the column stays as it is */ }
      poll();
    }, 3000);
  };

  // The composer posts with fetch; the answer is the page it returns to, and the column is swapped from it.
  chat.addEventListener('submit', async (e) => {
    const form = e.target;
    if (!form.matches('form.chat-form')) return;
    e.preventDefault();
    try {
      const r = await fetch(form.action, { ...same, method: form.method, body: new URLSearchParams(new FormData(form)), headers: { Accept: 'text/html' } });
      if (!r.ok) { form.submit(); return; } // the server refused: its page says why
      form.dataset.sent = '1';
      swap(await r.text());
    } catch (err) { /* the draft stays in the box */ }
  });

  chat.addEventListener('toggle', (e) => {
    try { localStorage.setItem(KEY, e.newState === 'open' ? '1' : '0'); } catch (err) { /* no storage */ }
  });
  try { if (localStorage.getItem(KEY) === '1') chat.showPopover(); } catch (err) { /* no storage */ }
  poll();
})();
