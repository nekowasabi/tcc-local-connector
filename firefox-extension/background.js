"use strict";

const BrowserPolicyVersion = 1;
const BrowserFailurePolicy = "fail_open";
const BrowserLivenessTTLSeconds = 15;
const NativeHostPollIntervalMilliseconds = 1000;
const NativeMessageHeaderBytes = 4;
const NativeMessageMaxPayloadBytes = 65536;
const NativeReconnectInitialSeconds = 1;
const NativeReconnectMaxSeconds = 30;
const BrowserPolicyMaxDomains = 128;
const BrowserPrivateWindowPolicy = "not_allowed";
const BrowserOwnerHeartbeatFileName = "firefox-owner-heartbeat.json";
const BrowserStateDirRelative = "TCCLocalConnector/BrowserPolicy";
const FirefoxNativeHostName = "jp.takets.tcc_local_connector.firefox";
const FirefoxExtensionID = "firefox-domain-blocker@tcc-local-connector.takets.jp";
const BlockedPagePath = "blocked.html";

function isIPv4(value) {
  const parts = value.split(".");
  return parts.length === 4 && parts.every((part) => {
    return /^\d{1,3}$/.test(part) && Number(part) <= 255;
  });
}

function normalizeDomain(value) {
  if (typeof value !== "string" || value.length === 0 || /[^\x00-\x7f]/.test(value)) {
    return null;
  }

  let domain = value.toLowerCase();
  if (domain.endsWith(".")) {
    domain = domain.slice(0, -1);
  }
  if (domain.length === 0 || domain.length > 253 || domain.endsWith(".") || isIPv4(domain)) {
    return null;
  }

  const labels = domain.split(".");
  for (const label of labels) {
    if (label.length === 0 || label.length > 63 ||
        !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label)) {
      return null;
    }
  }
  return domain;
}

function jsonByteLength(value) {
  try {
    return new TextEncoder().encode(JSON.stringify(value)).byteLength;
  } catch (_error) {
    return NativeMessageMaxPayloadBytes + 1;
  }
}

function validateDomainArray(value) {
  if (!Array.isArray(value) || value.length > BrowserPolicyMaxDomains) {
    return null;
  }
  const normalized = [];
  for (const domain of value) {
    const candidate = normalizeDomain(domain);
    if (candidate === null) {
      return null;
    }
    normalized.push(candidate);
  }
  return normalized;
}

function validatePolicyMessage(message) {
  if (message === null || typeof message !== "object" || Array.isArray(message) ||
      jsonByteLength(message) > NativeMessageMaxPayloadBytes) {
    return null;
  }

  const requiredKeys = ["type", "version", "generation", "enforce", "dry_run", "domains"];
  const allowedKeys = new Set([...requiredKeys, "planned_domains"]);
  const keys = Object.keys(message);
  if (!requiredKeys.every((key) => Object.prototype.hasOwnProperty.call(message, key)) ||
      keys.some((key) => !allowedKeys.has(key))) {
    return null;
  }
  if (message.type !== "policy" || message.version !== BrowserPolicyVersion ||
      !Number.isSafeInteger(message.generation) || message.generation < 0 ||
      typeof message.enforce !== "boolean" || typeof message.dry_run !== "boolean") {
    return null;
  }

  const domains = validateDomainArray(message.domains);
  const plannedDomains = Object.prototype.hasOwnProperty.call(message, "planned_domains")
    ? validateDomainArray(message.planned_domains)
    : [];
  if (domains === null || plannedDomains === null) {
    return null;
  }
  return {
    type: message.type,
    version: message.version,
    generation: message.generation,
    enforce: message.enforce,
    dry_run: message.dry_run,
    domains,
    planned_domains: plannedDomains,
  };
}

function isNewerGeneration(generation, lastGeneration) {
  return generation > lastGeneration;
}

function isBlockedHost(host, blockedDomains) {
  const normalizedHost = normalizeDomain(host);
  if (normalizedHost === null) {
    return false;
  }
  for (const domain of blockedDomains) {
    if (normalizedHost === domain || normalizedHost.endsWith(`.${domain}`)) {
      return true;
    }
  }
  return false;
}

function createBackground(browserApi, timerApi = globalThis) {
  let blockedDomains = new Set();
  let lastGeneration = -1;
  let lastAcceptedDomains = [];
  let nativePort = null;
  let reconnectTimer = null;
  let reconnectDelaySeconds = NativeReconnectInitialSeconds;

  function clearPolicy() {
    blockedDomains = new Set();
  }

  function handleNativeMessage(message, sourcePort = nativePort) {
    if (sourcePort !== nativePort) {
      return;
    }
    const policy = validatePolicyMessage(message);
    if (policy === null) {
      clearPolicy();
      return;
    }

    // Why: Invalidation precedes generation checks so an older stop signal can never leave stale blocking active.
    if (!policy.enforce || policy.dry_run || policy.domains.length === 0) {
      clearPolicy();
      return;
    }
    const recoveringSamePolicy = policy.generation === lastGeneration &&
      blockedDomains.size === 0 &&
      policy.domains.length === lastAcceptedDomains.length &&
      policy.domains.every((domain, index) => domain === lastAcceptedDomains[index]);
    // Why: Host liveness invalidation clears domains without advancing the
    // backend generation, so only the identical adopted policy may recover.
    if (!isNewerGeneration(policy.generation, lastGeneration) && !recoveringSamePolicy) {
      return;
    }
    blockedDomains = new Set(policy.domains);
    lastGeneration = policy.generation;
    lastAcceptedDomains = [...policy.domains];
  }

  function scheduleReconnect() {
    if (reconnectTimer !== null) {
      return;
    }
    const delaySeconds = reconnectDelaySeconds;
    reconnectDelaySeconds = Math.min(reconnectDelaySeconds * 2, NativeReconnectMaxSeconds);
    reconnectTimer = timerApi.setTimeout(() => {
      reconnectTimer = null;
      connectNative();
    }, delaySeconds * 1000);
  }

  function handleDisconnect(sourcePort = nativePort) {
    if (sourcePort !== nativePort) {
      return;
    }
    nativePort = null;
    clearPolicy();
      lastGeneration = -1;
      lastAcceptedDomains = [];
    scheduleReconnect();
  }

  function connectNative() {
    if (reconnectTimer !== null) {
      timerApi.clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    clearPolicy();
    lastGeneration = -1;

    let connectedPort;
    try {
      connectedPort = browserApi.runtime.connectNative(FirefoxNativeHostName);
      nativePort = connectedPort;
      reconnectDelaySeconds = NativeReconnectInitialSeconds;
      connectedPort.onMessage.addListener((message) => handleNativeMessage(message, connectedPort));
      connectedPort.onDisconnect.addListener(() => handleDisconnect(connectedPort));
      connectedPort.postMessage({type: "hello", version: BrowserPolicyVersion});
    } catch (_error) {
      if (nativePort === connectedPort) {
        nativePort = null;
      }
      scheduleReconnect();
    }
  }

  function blockedUrl(host) {
    const normalizedHost = normalizeDomain(host);
    if (normalizedHost === null) {
      return null;
    }
    return `${browserApi.runtime.getURL(BlockedPagePath)}?host=${encodeURIComponent(normalizedHost)}`;
  }

  function onBeforeRequest(details) {
    if (!details || details.type !== "main_frame" || details.incognito === true ||
        typeof details.url !== "string") {
      return {};
    }
    const extensionRoot = browserApi.runtime.getURL("");
    if (details.url.startsWith(extensionRoot)) {
      return {};
    }

    let requestUrl;
    try {
      requestUrl = new URL(details.url);
    } catch (_error) {
      return {};
    }
    if (!isBlockedHost(requestUrl.hostname, blockedDomains)) {
      return {};
    }
    return {redirectUrl: blockedUrl(requestUrl.hostname)};
  }

  function start() {
    browserApi.webRequest.onBeforeRequest.addListener(
      onBeforeRequest,
      {urls: ["<all_urls>"], types: ["main_frame"]},
      ["blocking"],
    );
    connectNative();
  }

  function getState() {
    return {
      blockedDomains: [...blockedDomains],
      lastGeneration,
      nativePort,
      reconnectTimer,
      reconnectDelaySeconds,
    };
  }

  return {
    blockedUrl,
    connectNative,
    getState,
    handleDisconnect,
    handleNativeMessage,
    onBeforeRequest,
    scheduleReconnect,
    start,
  };
}

const exported = {
  BlockedPagePath,
  BrowserFailurePolicy,
  BrowserLivenessTTLSeconds,
  BrowserOwnerHeartbeatFileName,
  BrowserPolicyMaxDomains,
  BrowserPolicyVersion,
  BrowserPrivateWindowPolicy,
  BrowserStateDirRelative,
  FirefoxExtensionID,
  FirefoxNativeHostName,
  NativeHostPollIntervalMilliseconds,
  NativeMessageHeaderBytes,
  NativeMessageMaxPayloadBytes,
  NativeReconnectInitialSeconds,
  NativeReconnectMaxSeconds,
  createBackground,
  isBlockedHost,
  isNewerGeneration,
  normalizeDomain,
  validatePolicyMessage,
};

if (typeof module !== "undefined") {
  module.exports = exported;
}
if (typeof browser !== "undefined") {
  createBackground(browser).start();
}
