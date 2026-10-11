// pwa.js: the installable surface's one script, and the SECOND entry of the script allowlist.
//
// It is linked by `pwaHead` on an ARMED deployment only, and what it may touch is pinned by
// `TestThePWAScriptTouchesOnlyWhatItSays` — a SPELLING guard, labelled as one; the browser
// test `TestPWAClauses/e_storage` is the STATE guard over what it actually stores.
//
//   READS   `navigator.standalone` (iOS's own flag — feature detection, never the user
//           agent), `display-mode: standalone`, and the ONE storage key below.
//   WRITES  `hidden` on the header's Install button, on the root page's iOS hint and on the
//           header's standalone Back/Reload (S5), and the ONE storage key — only on a dismiss
//           tap, only the value "1". No timestamp, no identity, no second key.
//   GOES    only where a tap on S5's Back or Reload asks, through exactly `history.back()` and
//           `location.reload()` — the browser's own two buttons, for a window that has none.
//   NEVER   a service worker (there is none in v1, by operator decision), a cache, a cookie,
//           a request or navigation of its own, or any API that turns text into markup or code.
//
// Without it nothing is lost: the controls and the hint are rendered `hidden` and stay hidden,
// an installed app is still one browser-menu click away, and an installed window keeps the
// wordmark and breadcrumbs as its way around.
(function () {
  "use strict";

  var HINT_KEY = "cairn.installHintDismissed";

  // An installed app's window is already the app, so it has nothing to install — and it has no
  // browser chrome either (S5): its Back and Reload are ours to show, and that is all done here.
  if (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) {
    revealStandaloneNav();
    return;
  }

  // Chromium: the browser decides the app is installable and says so with this event. The
  // button exists only to replay the browser's own prompt from a visible control.
  var button = document.getElementById("pwa-install");
  var deferred = null;
  if (button) {
    window.addEventListener("beforeinstallprompt", function (event) {
      event.preventDefault();
      deferred = event;
      button.hidden = false;
    });
    button.addEventListener("click", function () {
      var prompt = deferred;
      deferred = null;
      button.hidden = true;
      if (prompt) {
        prompt.prompt();
      }
    });
    window.addEventListener("appinstalled", function () {
      deferred = null;
      button.hidden = true;
    });
  }

  // iOS: there is no programmatic prompt, so the root page carries a one-line hint. It shows
  // only where iOS DEFINES `navigator.standalone` and reports false — a Safari tab.
  var hint = document.getElementById("pwa-install-hint");
  var dismiss = document.getElementById("pwa-install-hint-dismiss");
  if (!hint || !dismiss || !("standalone" in navigator) || navigator.standalone !== false) {
    return;
  }
  if (wasDismissed()) {
    return;
  }
  hint.hidden = false;
  dismiss.addEventListener("click", function () {
    hint.hidden = true;
    // Blocked storage throws; the hint then simply shows again on the next load.
    try {
      window.localStorage.setItem(HINT_KEY, "1");
    } catch (e) {
      return;
    }
  });

  // S5: both controls or neither, so a half-rendered header never offers Reload without Back.
  function revealStandaloneNav() {
    var back = document.getElementById("pwa-back");
    var reload = document.getElementById("pwa-reload");
    if (!back || !reload) {
      return;
    }
    back.addEventListener("click", function () {
      window.history.back();
    });
    reload.addEventListener("click", function () {
      window.location.reload();
    });
    back.hidden = false;
    reload.hidden = false;
  }

  function wasDismissed() {
    try {
      return window.localStorage.getItem(HINT_KEY) === "1";
    } catch (e) {
      return false;
    }
  }
})();
