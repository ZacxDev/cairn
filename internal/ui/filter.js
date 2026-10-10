// filter.js: the scope page's entry filter, and the ONE script this surface serves.
//
// What it may touch is pinned by `internal/ui`'s tests, and the list is short on purpose:
//
//   READS   the filter box's value, the `data-filter` attribute the SERVER rendered on
//           each entry row (ref, title, aliases and tags, one per line), and the text of each
//           row's server-rendered (hidden) alias chips. Nothing else on the page, and nothing
//           off it: no fetch, no XHR, no storage, no cookie.
//   WRITES  `hidden` on rows, on a row's alias chips (revealing an alias only when it is the
//           reason the row matched), on the empty-state line and on the control itself, and the
//           count line's `textContent`. Never `innerHTML`, `outerHTML`,
//           `insertAdjacentHTML`, `document.write`, `eval` or `Function`, so no user text
//           this file handles can become markup. `TestTheFilterScriptTouchesOnlyWhatItSays`
//           refuses those spellings in this file.
//
// Without it nothing is lost: the control is rendered `hidden` and stays hidden, so a page
// with script disabled shows every row and no filter box that does nothing.
(function () {
  "use strict";

  var control = document.getElementById("entry-filter-control");
  var input = document.getElementById("entry-filter");
  var list = document.getElementById("entry-list");
  var count = document.getElementById("entry-filter-count");
  var empty = document.getElementById("entry-filter-empty");
  if (!control || !input || !list || !count || !empty) {
    return;
  }
  var rows = Array.prototype.slice.call(list.querySelectorAll("li[data-filter]"));

  // Case-insensitive SUBSEQUENCE: every character of the term appears in the field in order,
  // not necessarily adjacent, so "rnbk" finds "runbook".
  function subsequence(term, field) {
    var i = 0;
    for (var j = 0; j < field.length && i < term.length; j++) {
      if (field.charAt(j) === term.charAt(i)) {
        i++;
      }
    }
    return i === term.length;
  }

  // A row is shown when EVERY whitespace-separated term matches SOME ONE field. Per field and
  // not over the joined string, so a term cannot match by borrowing its first letters from the
  // ref and its last from a tag.
  function rowMatches(terms, row) {
    var fields = (row.getAttribute("data-filter") || "").toLowerCase().split("\n");
    for (var t = 0; t < terms.length; t++) {
      var hit = false;
      for (var f = 0; f < fields.length && !hit; f++) {
        hit = subsequence(terms[t], fields[f]);
      }
      if (!hit) {
        return false;
      }
    }
    return true;
  }

  // Does SOME field match this term?
  function anyField(term, fields) {
    for (var f = 0; f < fields.length; f++) {
      if (subsequence(term, fields[f])) {
        return true;
      }
    }
    return false;
  }

  // The row's fields with its aliases taken out, ONE occurrence per alias: an alias that happens
  // to equal the ref still leaves the ref in.
  function withoutAliases(fields, aliases) {
    var rest = fields.slice();
    for (var a = 0; a < aliases.length; a++) {
      var at = rest.indexOf(aliases[a]);
      if (at >= 0) {
        rest.splice(at, 1);
      }
    }
    return rest;
  }

  // The card renders its aliases `hidden` (the server's choice) and this reveals ONLY the ones that
  // are WHY the row is shown: an alias matching a term that nothing else on the row matches. A row
  // kept by its ref, title or tags reveals none; every keystroke re-decides from scratch.
  function revealAliases(terms, row, keep) {
    var list = row.querySelector("ul.chips-alias");
    if (!list) {
      return;
    }
    var items = Array.prototype.slice.call(list.querySelectorAll("li"));
    var aliases = items.map(function (li) { return li.textContent.toLowerCase(); });
    var fields = (row.getAttribute("data-filter") || "").toLowerCase().split("\n");
    var rest = withoutAliases(fields, aliases);
    var any = false;
    for (var i = 0; i < items.length; i++) {
      var show = false;
      for (var t = 0; keep && t < terms.length && !show; t++) {
        show = subsequence(terms[t], aliases[i]) && !anyField(terms[t], rest);
      }
      items[i].hidden = !show;
      any = any || show;
    }
    list.hidden = !any;
  }

  function apply() {
    var terms = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    var shown = 0;
    for (var r = 0; r < rows.length; r++) {
      var keep = terms.length === 0 || rowMatches(terms, rows[r]);
      rows[r].hidden = !keep;
      revealAliases(terms, rows[r], keep && terms.length > 0);
      if (keep) {
        shown++;
      }
    }
    count.textContent = shown + " of " + rows.length + (rows.length === 1 ? " entry" : " entries");
    empty.hidden = shown !== 0;
  }

  input.addEventListener("input", apply);
  control.hidden = false;
  // A value the browser restored on back/forward navigation is applied at once, so the list
  // never disagrees with the box.
  apply();
})();
