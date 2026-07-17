// Loaded as a sub-resource over the same app:// origin as index.html — proof
// that relative asset requests are answered by the scheme handler too, not just
// the entry page.
(async () => {
  const set = (id, ok, text) => {
    const el = document.getElementById(id);
    el.textContent = text;
    el.className = ok ? "ok" : "bad";
  };

  // This script ran, so the sub-resource was served.
  set("subresource", true, "loaded ✓");

  document.getElementById("origin").textContent = location.origin || location.href;

  set("secure", window.isSecureContext,
    window.isSecureContext ? "true" : "false — not a secure context");

  // localStorage and crypto.subtle are both gated behind a secure context; they
  // work here only because the app:// scheme was registered as secure.
  try {
    localStorage.setItem("glaze", "hello");
    const v = localStorage.getItem("glaze");
    set("storage", v === "hello", v === "hello" ? `read back "${v}"` : "unavailable");
  } catch (e) {
    set("storage", false, `unavailable: ${e}`);
  }

  try {
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode("glaze"));
    const hex = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
    set("crypto", true, `${hex.slice(0, 16)}…`);
  } catch (e) {
    set("crypto", false, `unavailable: ${e}`);
  }
})();
