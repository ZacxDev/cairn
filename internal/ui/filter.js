// filter.js: the scope page's entry filter, and the ONE script this surface serves.
//
// What it may touch is pinned by `internal/ui`'s tests, and the list is short on purpose:
//
//   READS   the filter box's value, and the `data-filter` attribute the SERVER rendered on
//           each entry row (ref, title, aliases and tags, one per line). Nothing else on the
//           page, and nothing off it: no fetch, no XHR, no storage, no cookie.
//   WRITES  `hidden` on rows, on the empty-state line and on the control itself, and the
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

  function apply() {
    var terms = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    var shown = 0;
    for (var r = 0; r < rows.length; r++) {
      var keep = terms.length === 0 || rowMatches(terms, rows[r]);
      rows[r].hidden = !keep;
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
