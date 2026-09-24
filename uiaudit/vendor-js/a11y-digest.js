// Canonical source for the CRAWL-PATH A11y/DOM DIGEST (persona-evaluator grounding,
// Phase 1). Returns a BOUNDED JSON string describing the page's semantic structure
// from the DOM / accessibility tree — the AUTHORITATIVE facts about labels, roles,
// accessible names, focusability and keyboard-operability that a screenshot cannot
// reveal (an sr-only label, an <a> that looks like a card, a resolved aria name).
// The persona evaluator reads this so it stops re-deriving mechanical a11y from
// pixels (the meta-audit's whole objective-false-positive cluster).
//
// It is captured alongside axe on the SAME default tab (one extra Evaluate, no new
// tab — respects the chromium-149 second-tab capture gotcha). Best-effort: the whole
// body is try/caught so an exotic/hostile page degrades to an empty digest rather
// than breaking capture; on the Go side a failure is non-fatal.
//
// SECURITY: read-only (never mutates the page). Its OUTPUT is UNTRUSTED — selectors
// and names round-trip only as data (stored as escaped JSON, rendered escaped),
// never back through eval.
//
// BUDGET: hard caps (<=40 interactive, <=30 form controls, <=30 landmarks/headings)
// + truncation (names 120, text 80, selectors 200) keep the payload ~<=16KB.
(() => {
 try {
  const MAX_INTERACTIVE = 40;
  const MAX_FORM = 30;
  const MAX_LANDMARK = 30;

  const sel = (el) => {
    if (!el || el.nodeType !== 1) return '';
    if (el.id) return el.tagName.toLowerCase() + '#' + CSS.escape(el.id);
    const nm = el.getAttribute && el.getAttribute('name');
    if (nm) return el.tagName.toLowerCase() + '[name="' + nm.replace(/"/g, '\\"') + '"]';
    const cls = (el.className && typeof el.className === 'string')
      ? '.' + el.className.trim().split(/\s+/).slice(0, 2).map(CSS.escape).join('.') : '';
    return el.tagName.toLowerCase() + cls;
  };

  const visible = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) return false;
    const st = getComputedStyle(el);
    return st.visibility !== 'hidden' && st.display !== 'none' && parseFloat(st.opacity || '1') > 0.01;
  };

  // nameAndSource computes a best-effort accessible name + how it was derived,
  // mirroring the browser's own name computation order: aria-label >
  // aria-labelledby > associated <label> (el.labels covers BOTH <label for> and a
  // wrapping <label>) > visible text (non-form controls) > placeholder > value. This
  // is the positive fact axe cannot give (a present sr-only label is a NON-violation,
  // so it never appears in axe output — only a DOM read reveals it).
  const nameAndSource = (el) => {
    const aria = el.getAttribute('aria-label');
    if (aria && aria.trim()) return { name: aria.trim(), source: 'aria-label' };
    const lb = el.getAttribute('aria-labelledby');
    if (lb) {
      const parts = lb.split(/\s+/).map((id) => {
        const n = document.getElementById(id);
        return n ? (n.textContent || '').trim() : '';
      }).filter(Boolean);
      if (parts.length) return { name: parts.join(' '), source: 'aria-labelledby' };
    }
    if (el.labels && el.labels.length) {
      const txt = Array.from(el.labels).map((l) => (l.textContent || '').trim()).filter(Boolean).join(' ');
      if (txt) {
        let src = 'wrapping-label';
        for (const l of el.labels) { if (l.hasAttribute && l.hasAttribute('for')) { src = 'for'; break; } }
        return { name: txt, source: src };
      }
    }
    const tag = el.tagName;
    if (tag !== 'INPUT' && tag !== 'SELECT' && tag !== 'TEXTAREA') {
      const txt = (el.textContent || '').trim().replace(/\s+/g, ' ');
      if (txt) return { name: txt, source: 'text' };
    }
    const ph = el.getAttribute('placeholder');
    if (ph && ph.trim()) return { name: ph.trim(), source: 'placeholder' };
    const val = el.getAttribute('value');
    if (val && val.trim() && (el.type === 'submit' || el.type === 'button')) return { name: val.trim(), source: 'value' };
    return { name: '', source: 'none' };
  };

  // focusable: natively focusable (a[href]/button/input/select/textarea report
  // tabIndex 0) or an explicit tabindex >= 0, and not disabled. An <a> without href
  // reports tabIndex -1 → not focusable, which is the honest answer.
  const isFocusable = (el) => {
    if (el.disabled) return false;
    const ti = typeof el.tabIndex === 'number' ? el.tabIndex : -1;
    return ti >= 0;
  };

  // A "true" programmatic label association (what a screen reader announces as a
  // label) — placeholder/value/visible-text are NOT proper labels for a form control.
  const labelSources = { 'for': true, 'aria-label': true, 'aria-labelledby': true, 'wrapping-label': true };

  const interactiveQ = 'a[href], button, input, select, textarea, [role=button], [role=link], [role=tab], [role=checkbox], [role=radio], [role=menuitem], [role=switch], [contenteditable=true]';
  const interactive = [];
  for (const el of document.querySelectorAll(interactiveQ)) {
    if (interactive.length >= MAX_INTERACTIVE) break;
    if (!visible(el)) continue;
    const type = (el.getAttribute('type') || '');
    if (type === 'hidden') continue;
    const ns = nameAndSource(el);
    // label_source (#36): expose the derivation on EVERY interactive element so the
    // deterministic verify can soundly refute an accessible-name-absence finding on a
    // button/link, not just a form control. Normalize the internal name sources into
    // the documented closed set: the raw 'text'/'value' (element's own content/attr,
    // NOT a programmatic label — an icon <button>★</button> is 'text-content') collapse
    // to 'text-content'; the programmatic sources (for/aria-label/aria-labelledby/
    // wrapping-label) and placeholder/none pass through unchanged.
    const labelSource = (ns.source === 'text' || ns.source === 'value') ? 'text-content' : ns.source;
    interactive.push({
      tag: el.tagName.toLowerCase(),
      role: el.getAttribute('role') || '',
      accessible_name: ns.name.slice(0, 120),
      selector: sel(el).slice(0, 200),
      type: type,
      disabled: !!(el.disabled || el.getAttribute('aria-disabled') === 'true'),
      focusable: isFocusable(el),
      label_source: labelSource,
    });
  }

  const formControls = [];
  for (const el of document.querySelectorAll('input, select, textarea')) {
    if (formControls.length >= MAX_FORM) break;
    const type = (el.getAttribute('type') || '');
    if (type === 'hidden') continue;
    if (!visible(el)) continue;
    const ns = nameAndSource(el);
    // A form control never legitimately takes its name from body text — collapse a
    // stray 'text' source to 'none' so labelSource stays in the documented set.
    const source = (ns.source === 'text' || ns.source === 'value') ? 'none' : ns.source;
    formControls.push({
      selector: sel(el).slice(0, 200),
      accessible_name: ns.name.slice(0, 120),
      has_label: !!labelSources[source],
      label_source: source,
    });
  }

  // Landmark + heading skeleton, in document order (querySelectorAll is DOM order).
  // Cheap and bounded — gives the persona the page's structural scaffolding.
  const landmarks = [];
  const landmarkQ = 'header, nav, main, aside, footer, section[aria-label], section[aria-labelledby], [role=banner], [role=navigation], [role=main], [role=complementary], [role=contentinfo], [role=search], [role=region], h1, h2, h3, h4, h5, h6';
  for (const el of document.querySelectorAll(landmarkQ)) {
    if (landmarks.length >= MAX_LANDMARK) break;
    if (!visible(el)) continue;
    landmarks.push({
      tag: el.tagName.toLowerCase(),
      role: el.getAttribute('role') || '',
      text: (el.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 80),
    });
  }

  return JSON.stringify({ interactive: interactive, form_controls: formControls, landmarks: landmarks });
 } catch (e) {
  return '{"interactive":[],"form_controls":[],"landmarks":[]}';
 }
})()
