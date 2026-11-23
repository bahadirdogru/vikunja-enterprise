(function () {
  var root = document.documentElement;
  var btn = document.getElementById('theme-toggle');
  function current() {
    var t = root.getAttribute('data-theme');
    if (t) return t;
    return window.matchMedia && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  function label() {
    if (btn) btn.setAttribute('aria-label', current() === 'dark' ? 'Switch to light theme' : 'Switch to dark theme');
  }
  if (btn) {
    btn.hidden = false;
    label();
    btn.addEventListener('click', function () {
      var next = current() === 'dark' ? 'light' : 'dark';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem('ve-theme', next); } catch (e) {}
      label();
    });
  }
  var copy = document.getElementById('copy-btn');
  var status = document.getElementById('copy-status');
  var code = document.getElementById('docker-code');
  if (copy && code) {
    copy.hidden = false;
    copy.addEventListener('click', function () {
      var text = code.textContent;
      function done(ok) {
        status.textContent = ok ? 'Copied to clipboard.' : 'Copy failed. Select the text and copy it manually.';
        copy.textContent = ok ? 'Copied' : 'Copy';
        setTimeout(function () { copy.textContent = 'Copy'; status.textContent = ''; }, 2500);
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(function () { done(true); }, function () { done(false); });
      } else {
        try {
          var ta = document.createElement('textarea');
          ta.value = text; ta.setAttribute('readonly', '');
          ta.style.position = 'fixed'; ta.style.opacity = '0';
          document.body.appendChild(ta); ta.select();
          var ok = document.execCommand('copy');
          document.body.removeChild(ta); done(ok);
        } catch (e) { done(false); }
      }
    });
  }
})();
