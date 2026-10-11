// join.js: the join page's one script, and the THIRD entry of the script allowlist.
//
// A team link is `/join#invite=<token>`. A browser never sends the part after `#` to any
// server, so no access log at any hop can hold the token. This script moves the token from
// the fragment into the accept form's one hidden field; the person's click on the accept
// button then POSTs it to the provider start row, which carries it on the server-side flight.
//
// What it may touch is pinned by `TestTheJoinScriptTouchesOnlyWhatItSays`, a SPELLING guard
// labelled as one; uiaudit's `TestTheJoinFragmentIsClearedAndPosted` is the browser test.
//
//   READS   `location.hash`, as the first thing each run does.
//   WRITES  the history entry's URL, IMMEDIATELY after that read, to the literal join path, so
//           the token leaves the address bar and the history entry before anything else runs.
//           Then the accept form's hidden field's `value`, and `hidden` on two blocks — and, on a
//           `pageshow` that restores the page from the back-forward cache, the field EMPTIED and
//           the form hidden again, so Back from the accept POST never shows a filled form.
//   NEVER   a navigation, a form submission or retarget, a request, storage, or any API that
//           turns text into markup or code. Nothing read from the URL can become a target.
//
// It runs on load AND on `hashchange`: pasting the whole link into a tab already showing
// `/join` (the remedy the no-invitation sentence asks for) is a same-document navigation,
// which does not reload the page.
//
// Without it the page shows its `<noscript>` notice, so a link opened with script off fails
// visibly rather than silently.
(function () {
  "use strict";

  take();
  window.addEventListener("hashchange", take);
  window.addEventListener("pageshow", restored);

  function take() {
    var hash = location.hash;
    history.replaceState(null, "", "/join");

    var accept = document.getElementById("join-accept");
    var field = document.getElementById("join-invite");
    var missing = document.getElementById("join-missing");
    if (!accept || !field || !missing) {
      return;
    }
    var token = tokenFrom(hash);
    if (token === "") {
      // A fragment-less run after a token arrived changes nothing.
      if (field.value === "") {
        missing.hidden = false;
      }
      return;
    }
    field.value = token;
    missing.hidden = true;
    accept.hidden = false;
  }

  // tokenFrom answers the token a `#invite=<token>` fragment carries, or "" for any other shape.
  function tokenFrom(fragment) {
    var prefix = "#invite=";
    if (fragment.indexOf(prefix) !== 0) {
      return "";
    }
    try {
      return decodeURIComponent(fragment.slice(prefix.length));
    } catch (e) {
      return "";
    }
  }

  // restored: Back from the accept POST can restore this page from the back-forward cache with
  // the field still FILLED and the form shown. The URL is already the bare join path, so the
  // restored page is put back in that state: the field emptied, the form hidden, the
  // no-invitation sentence shown. A fresh load (`persisted` false) is `take`'s job.
  function restored(event) {
    if (!event.persisted) {
      return;
    }
    var accept = document.getElementById("join-accept");
    var field = document.getElementById("join-invite");
    var missing = document.getElementById("join-missing");
    if (!accept || !field || !missing) {
      return;
    }
    field.value = "";
    accept.hidden = true;
    missing.hidden = false;
  }
})();
