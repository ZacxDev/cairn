// Canonical source. External push harnesses (external ux-audit harnesses) copy this
// verbatim and must keep it in sync — the finding shape it returns is the P5 push
// contract (the type=layout findings + crawler.LayoutSmells JSON).
//
// Computes deterministic DOM layout smells for the CURRENT viewport and returns them
// (counts + up to a few example selectors each) as a JSON string matching
// crawler.LayoutSmells. Tap-target / small-text checks read computed geometry/style;
// horizontal overflow compares scrollWidth to innerWidth. The whole body is wrapped
// in try/catch: a hostile/exotic page that throws must degrade to zero smells, never
// drop the shared capture.
(() => {
 try {
  const examples = {};
  const push = (k, v) => { (examples[k] = examples[k] || []); if (examples[k].length < 5 && v) examples[k].push(v); };
  const sel = (el) => {
    if (!el || el.nodeType !== 1) return '';
    if (el.id) return el.tagName.toLowerCase() + '#' + el.id;
    const cls = (el.className && typeof el.className === 'string') ? '.' + el.className.trim().split(/\s+/).slice(0,2).join('.') : '';
    return el.tagName.toLowerCase() + cls;
  };
  const iw = window.innerWidth || document.documentElement.clientWidth;
  const scrollW = document.documentElement.scrollWidth;
  const overflow = scrollW > iw + 4; // small tolerance for sub-pixel rounding
  if (overflow) push('horizontal_overflow', 'scrollWidth=' + scrollW + ' > innerWidth=' + iw);

  let smallTap = 0;
  for (const el of document.querySelectorAll('a, button, input, select, [role=button]')) {
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue; // not rendered
    if (r.width < 44 || r.height < 44) { smallTap++; push('small_tap_targets', sel(el) + ' (' + Math.round(r.width) + 'x' + Math.round(r.height) + ')'); }
  }

  let smallText = 0;
  const hasDirectText = (el) => {
    for (const n of el.childNodes) if (n.nodeType === 3 && n.textContent.trim().length) return true;
    return false;
  };
  for (const el of document.querySelectorAll('body *')) {
    if (!hasDirectText(el)) continue;
    const fs = parseFloat(getComputedStyle(el).fontSize);
    if (fs && fs < 12) { smallText++; push('small_text', sel(el) + ' (' + Math.round(fs) + 'px)'); }
    if (smallText > 500) break; // bound the scan on huge pages
  }

  const missingViewport = !document.querySelector('meta[name=viewport]');

  let imgNoDims = 0;
  for (const img of document.querySelectorAll('img')) {
    if (!img.hasAttribute('width') && !img.hasAttribute('height')) { imgNoDims++; push('images_no_dims', sel(img) + (img.currentSrc ? ' src=' + img.currentSrc.slice(0,80) : '')); }
  }

  return JSON.stringify({
    horizontal_overflow: overflow, scroll_width: scrollW, inner_width: iw,
    small_tap_targets: smallTap, small_text: smallText,
    missing_viewport_meta: missingViewport, images_no_dims: imgNoDims,
    examples,
  });
 } catch (e) {
  // A hostile/exotic page that throws from getComputedStyle/getBoundingClientRect/
  // querySelectorAll must NOT drop the whole capture (this eval shares one
  // chromedp.Run with the screenshot + axe evals) — degrade to zero layout smells.
  return '{}';
 }
})()
