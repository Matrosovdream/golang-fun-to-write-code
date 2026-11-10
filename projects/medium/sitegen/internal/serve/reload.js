const es = new EventSource("/__reload");
es.addEventListener("reload", () => location.reload());
es.addEventListener("build-error", (e) => {
  let box = document.getElementById("__sitegen_error");
  if (!box) {
    box = document.body.appendChild(document.createElement("pre"));
    box.id = "__sitegen_error";
    box.style.cssText = "position:fixed;left:0;right:0;bottom:0;margin:0;padding:1rem;background:#b00020;color:#fff;font:14px monospace;white-space:pre-wrap";
  }
  box.textContent = e.data;
});
