"use strict";

const BlockedPageReason = "現在のタスクにより、このドメインは遮断中です。";

function normalizeBlockedHost(value) {
  if (typeof value !== "string" || value.length === 0 || /[^\x00-\x7f]/.test(value)) {
    return "";
  }
  let host = value.toLowerCase();
  if (host.endsWith(".")) {
    host = host.slice(0, -1);
  }
  if (host.length === 0 || host.length > 253 || host.endsWith(".") || /[:/?#@]/.test(host)) {
    return "";
  }
  const labels = host.split(".");
  if (labels.some((label) => label.length === 0 || label.length > 63 ||
      !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label))) {
    return "";
  }
  return host;
}

function readBlockedHost(search) {
  const params = new URLSearchParams(search);
  return normalizeBlockedHost(params.get("host"));
}

function renderBlockedHost(documentObject, search) {
  const hostElement = documentObject.getElementById("blocked-host");
  const reasonElement = documentObject.getElementById("blocked-reason");
  hostElement.textContent = readBlockedHost(search);
  reasonElement.textContent = BlockedPageReason;
}

if (typeof module !== "undefined") {
  module.exports = {BlockedPageReason, normalizeBlockedHost, readBlockedHost, renderBlockedHost};
}
if (typeof document !== "undefined" && typeof window !== "undefined") {
  document.addEventListener("DOMContentLoaded", () => renderBlockedHost(document, window.location.search));
}
