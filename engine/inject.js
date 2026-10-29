// Injected into the page by xUse. Guarded so it installs once per document.
(() => {
  if (window.__xuse && window.__xuse.v === 1) return;

  const X = { v: 1, refs: new Map(), seq: 0 };

  const INTERACTIVE = new Set([
    'button', 'link', 'checkbox', 'radio', 'tab', 'menuitem', 'menuitemcheckbox',
    'menuitemradio', 'option', 'combobox', 'textbox', 'searchbox', 'switch',
    'slider', 'spinbutton', 'listbox', 'treeitem',
  ]);
  const TESTID_ATTRS = ['data-testid', 'data-test-id', 'data-test', 'data-qa', 'data-cy'];

  const clean = (s) => (s || '').replace(/\s+/g, ' ').trim().slice(0, 80);

  // All elements in document order, including open shadow roots.
  function all(root = document, out = []) {
    for (const el of root.children) {
      out.push(el);
      if (el.shadowRoot) all(el.shadowRoot, out);
      all(el, out);
    }
    return out;
  }

  function role(el) {
    const r = el.getAttribute('role');
    if (r) return r.split(/\s+/)[0];
    const t = el.tagName.toLowerCase();
    if (t === 'a' && el.hasAttribute('href')) return 'link';
    if (t === 'button' || t === 'summary') return 'button';
    if (t === 'select') return el.multiple || el.size > 1 ? 'listbox' : 'combobox';
    if (t === 'textarea') return 'textbox';
    if (t === 'input') {
      const ty = (el.getAttribute('type') || 'text').toLowerCase();
      if (['button', 'submit', 'reset', 'image'].includes(ty)) return 'button';
      if (ty === 'checkbox') return 'checkbox';
      if (ty === 'radio') return 'radio';
      if (ty === 'range') return 'slider';
      if (ty === 'number') return 'spinbutton';
      if (ty === 'search') return 'searchbox';
      if (ty === 'hidden') return '';
      return 'textbox';
    }
    if (/^h[1-6]$/.test(t)) return 'heading';
    if (el.isContentEditable && el.hasAttribute('contenteditable')) return 'textbox';
    return '';
  }

  function byId(el, id) {
    const root = el.getRootNode();
    return (root.getElementById ? root : document).getElementById(id);
  }

  function name(el) {
    const aria = clean(el.getAttribute('aria-label'));
    if (aria) return aria;
    const lb = el.getAttribute('aria-labelledby');
    if (lb) {
      const t = clean(lb.split(/\s+/).map((id) => (byId(el, id) || {}).innerText || '').join(' '));
      if (t) return t;
    }
    if (el.labels && el.labels.length) {
      const t = clean([...el.labels].map((l) => l.innerText).join(' '));
      if (t) return t;
    }
    const t = el.tagName;
    if (t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT') {
      const ty = (el.getAttribute('type') || '').toLowerCase();
      if (['button', 'submit', 'reset'].includes(ty) && el.value) return clean(el.value);
      return clean(el.getAttribute('placeholder') || el.getAttribute('title'));
    }
    return clean(el.innerText || el.textContent) || clean(el.getAttribute('title') || el.getAttribute('alt'));
  }

  function visible(el) {
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) return false;
    const s = getComputedStyle(el);
    return s.visibility !== 'hidden' && s.display !== 'none' && s.opacity !== '0';
  }

  function interactive(el) {
    if (INTERACTIVE.has(role(el))) return true;
    if (el.hasAttribute('onclick')) return true;
    return el.hasAttribute('tabindex') && el.tabIndex >= 0;
  }

  function refOf(el) {
    if (el.__xuseRef && X.refs.get(el.__xuseRef) === el) return el.__xuseRef;
    const ref = 'e' + (++X.seq);
    el.__xuseRef = ref;
    X.refs.set(ref, el);
    return ref;
  }

  function get(ref) {
    const el = X.refs.get(ref);
    return el && el.isConnected ? el : null;
  }

  // ---- snapshot -----------------------------------------------------------

  X.snapshot = (max = 300) => {
    const lines = [];
    let more = 0;
    for (const el of all()) {
      const r = role(el);
      if (r === 'heading') {
        if (!visible(el)) continue;
        const t = clean(el.innerText);
        if (t) lines.push('#'.repeat(+el.tagName[1] || 2) + ' ' + t);
        continue;
      }
      if (!interactive(el) || !visible(el)) continue;
      if (lines.length >= max) { more++; continue; }
      let line = `[${refOf(el)}] ${r || el.tagName.toLowerCase()}`;
      const n = name(el);
      if (n) line += ` "${n}"`;
      if (r === 'textbox' || r === 'searchbox' || r === 'spinbutton' || r === 'combobox') {
        const v = el.isContentEditable ? el.innerText : el.value;
        if (v) line += ` value="${clean(r === 'combobox' && el.selectedOptions ? el.selectedOptions[0]?.text : v)}"`;
      }
      if (el.checked) line += ' checked';
      if (el.disabled) line += ' disabled';
      lines.push(line);
    }
    if (more) lines.push(`… ${more} more`);
    return { url: location.href, title: document.title, lines };
  };

  // ---- locators -----------------------------------------------------------

  function cssPath(el) {
    const parts = [];
    for (let e = el; e && e.nodeType === 1 && e !== document.documentElement; e = e.parentElement) {
      let p = e.tagName.toLowerCase();
      if (e.id && !generatedId(e.id)) { parts.unshift('#' + CSS.escape(e.id)); break; }
      const sibs = e.parentElement ? [...e.parentElement.children].filter((s) => s.tagName === e.tagName) : [];
      if (sibs.length > 1) p += `:nth-of-type(${sibs.indexOf(e) + 1})`;
      parts.unshift(p);
    }
    return parts.join(' > ');
  }

  const generatedId = (id) => /\d{4,}|^[a-f0-9-]{12,}$|[:]/i.test(id);

  X.locator = (ref) => {
    const el = get(ref);
    if (!el) return null;
    const L = { role: role(el), name: name(el), tag: el.tagName.toLowerCase() };
    for (const a of TESTID_ATTRS) {
      const v = el.getAttribute(a);
      if (v) { L.testidAttr = a; L.testid = v; break; }
    }
    if (el.id && !generatedId(el.id)) L.id = el.id;
    if (el.getAttribute('name')) L.nameAttr = el.getAttribute('name');
    if (el.getAttribute('placeholder')) L.placeholder = el.getAttribute('placeholder');
    if (el.getRootNode() === document) L.css = cssPath(el);
    const hit = resolve(L);
    L.via = hit && hit.el === el ? hit.via : '';
    return L;
  };

  // Strategies in priority order. The first one with exactly one visible match wins.
  const STRATEGIES = [
    ['testid', (L) => L.testid && ((el) => el.getAttribute(L.testidAttr || 'data-testid') === L.testid)],
    ['id', (L) => L.id && ((el) => el.id === L.id)],
    ['nameAttr', (L) => L.nameAttr && ((el) => el.getAttribute('name') === L.nameAttr && el.tagName.toLowerCase() === L.tag)],
    ['role+name', (L) => L.role && L.name && ((el) => role(el) === L.role && name(el) === L.name)],
    ['placeholder', (L) => L.placeholder && ((el) => el.getAttribute('placeholder') === L.placeholder)],
  ];

  function resolve(L) {
    const els = all().filter(visible);
    for (const [via, make] of STRATEGIES) {
      const pred = make(L);
      if (!pred) continue;
      const m = els.filter(pred);
      if (m.length === 1) return { el: m[0], via };
    }
    if (L.css) {
      try {
        const m = [...document.querySelectorAll(L.css)].filter(visible);
        if (m.length === 1) return { el: m[0], via: 'css' };
      } catch (_) { /* bad selector */ }
    }
    return null;
  }

  // Wait until the locator resolves; returns {ref, via} or null on timeout.
  X.find = (L, timeout = 5000) => new Promise((done) => {
    const t0 = performance.now();
    const tick = () => {
      const hit = resolve(L);
      if (hit) return done({ ref: refOf(hit.el), via: hit.via });
      if (performance.now() - t0 > timeout) return done(null);
      setTimeout(tick, 50);
    };
    tick();
  });

  // ---- actions --------------------------------------------------------------

  // Scroll into view and return the click point, plus whether the element is on top.
  X.point = (ref) => {
    const el = get(ref);
    if (!el) return null;
    el.scrollIntoView({ block: 'center', inline: 'center' });
    const r = el.getBoundingClientRect();
    const x = r.left + r.width / 2, y = r.top + r.height / 2;
    const root = el.getRootNode();
    const hit = (root.elementFromPoint ? root : document).elementFromPoint(x, y);
    return { x, y, onTop: !!hit && (hit === el || el.contains(hit) || hit.contains(el)) };
  };

  X.click = (ref) => { const el = get(ref); if (!el) return false; el.click(); return true; };

  // Clear the field and focus it; the text itself is typed via CDP Input.insertText.
  // Returns '' on success, else the reason.
  X.clearAndFocus = (ref) => {
    const el = get(ref);
    if (!el) return 'stale';
    if (!el.isContentEditable && el.tagName !== 'INPUT' && el.tagName !== 'TEXTAREA') {
      return `not a text field (${role(el) || el.tagName.toLowerCase()}); use select or click`;
    }
    el.scrollIntoView({ block: 'center' });
    el.focus();
    if (el.isContentEditable) {
      document.getSelection().selectAllChildren(el);
      document.execCommand('delete');
      return '';
    }
    const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, '');
    el.dispatchEvent(new Event('input', { bubbles: true }));
    return '';
  };

  // Fire change and return the field's value, so the caller can check it took.
  X.changed = (ref) => {
    const el = get(ref);
    if (!el) return null;
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return el.isContentEditable ? el.innerText : el.value;
  };

  X.select = (ref, value) => {
    const el = get(ref);
    if (!el || el.tagName !== 'SELECT') return 'not a <select>';
    const opt = [...el.options].find((o) => o.value === value) ||
      [...el.options].find((o) => clean(o.text) === clean(value));
    if (!opt) return 'no option ' + JSON.stringify(value);
    el.value = opt.value;
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return '';
  };

  X.hasText = (text, timeout = 5000) => new Promise((done) => {
    const t0 = performance.now();
    const tick = () => {
      if ((document.body.innerText || '').includes(text)) return done(true);
      if (performance.now() - t0 > timeout) return done(false);
      setTimeout(tick, 50);
    };
    tick();
  });

  // Resolve once the DOM has been quiet for `quiet` ms (or `max` ms passed).
  X.settle = (quiet = 120, max = 3000) => new Promise((done) => {
    const t0 = performance.now();
    let last = t0;
    const mo = new MutationObserver(() => { last = performance.now(); });
    mo.observe(document, { subtree: true, childList: true, attributes: true, characterData: true });
    const tick = () => {
      const now = performance.now();
      if ((now - last >= quiet && document.readyState === 'complete') || now - t0 >= max) {
        mo.disconnect();
        return done(Math.round(now - t0));
      }
      setTimeout(tick, 30);
    };
    tick();
  });

  window.__xuse = X;
})();
