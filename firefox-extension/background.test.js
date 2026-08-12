"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  FirefoxNativeHostName,
  NativeMessageMaxPayloadBytes,
  createBackground,
  isBlockedHost,
  normalizeDomain,
  validatePolicyMessage,
} = require("./background.js");
const {
  BlockedPageReason,
  readBlockedHost,
  renderBlockedHost,
} = require("./blocked.js");

function policy(overrides = {}) {
  return {
    type: "policy",
    version: 1,
    generation: 1,
    enforce: true,
    dry_run: false,
    domains: ["example.com"],
    planned_domains: ["example.com"],
    ...overrides,
  };
}

function event() {
  const listeners = [];
  return {
    addListener(listener) {
      listeners.push(listener);
    },
    emit(value) {
      for (const listener of listeners) {
        listener(value);
      }
    },
    listeners,
  };
}

function port() {
  return {
    onMessage: event(),
    onDisconnect: event(),
    sent: [],
    postMessage(message) {
      this.sent.push(message);
    },
  };
}

function harness() {
  const ports = [];
  const requests = event();
  const timers = [];
  const browserApi = {
    runtime: {
      connectNative(name) {
        assert.equal(name, FirefoxNativeHostName);
        const nextPort = port();
        ports.push(nextPort);
        return nextPort;
      },
      getURL(path) {
        return `moz-extension://extension-id/${path}`;
      },
    },
    webRequest: {onBeforeRequest: requests},
  };
  const timerApi = {
    setTimeout(callback, milliseconds) {
      const timer = {callback, milliseconds, cleared: false};
      timers.push(timer);
      return timer;
    },
    clearTimeout(timer) {
      timer.cleared = true;
    },
  };
  return {background: createBackground(browserApi, timerApi), browserApi, ports, requests, timers};
}

test("wire hello uses a Port JSON object and no extension codec", () => {
  const setup = harness();
  setup.background.start();
  assert.deepEqual(setup.ports[0].sent, [{type: "hello", version: 1}]);
  assert.equal(typeof require("./background.js").encodeNativeMessage, "undefined");
  assert.equal(typeof require("./background.js").decodeNativeMessage, "undefined");
});

test("policy schema, payload, and domain boundaries are strict", () => {
  assert.deepEqual(validatePolicyMessage(policy()).domains, ["example.com"]);
  assert.deepEqual(validatePolicyMessage(policy({domains: ["EXAMPLE.COM."]})).domains, ["example.com"]);
  for (const candidate of [
    null,
    [],
    {...policy(), type: "hello"},
    {...policy(), version: 2},
    {...policy(), generation: -1},
    {...policy(), generation: 1.5},
    {...policy(), enforce: 1},
    {...policy(), dry_run: 0},
    {...policy(), domains: null},
    {...policy(), planned_domains: null},
    {...policy(), extra: true},
    {...policy(), domains: Array(129).fill("example.com")},
    {...policy(), planned_domains: Array(129).fill("example.com")},
  ]) {
    assert.equal(validatePolicyMessage(candidate), null);
  }
  const oversized = policy({planned_domains: ["a".repeat(NativeMessageMaxPayloadBytes)]});
  assert.equal(validatePolicyMessage(oversized), null);
});

test("policy domain validation rejects URL, path, port, wildcard, IP, non-ASCII, and empty labels", () => {
  for (const invalid of [
    "https://example.com",
    "example.com/path",
    "example.com:443",
    "*.example.com",
    "127.0.0.1",
    "2001:db8::1",
    "例.example",
    "a..example",
    "-a.example",
    "a-.example",
  ]) {
    assert.equal(normalizeDomain(invalid), null, invalid);
    assert.equal(validatePolicyMessage(policy({domains: [invalid]})), null, invalid);
  }
});

test("generation only replaces with newer policy", () => {
  const setup = harness();
  setup.background.connectNative();
  const connectedPort = setup.ports[0];
  connectedPort.onMessage.emit(policy({generation: 3, domains: ["three.example"]}));
  connectedPort.onMessage.emit(policy({generation: 3, domains: ["same.example"]}));
  assert.deepEqual(setup.background.getState().blockedDomains, ["three.example"]);
  connectedPort.onMessage.emit(policy({generation: 2, domains: ["old.example"]}));
  assert.deepEqual(setup.background.getState().blockedDomains, ["three.example"]);
  connectedPort.onMessage.emit(policy({generation: 4, domains: ["four.example"]}));
  assert.deepEqual(setup.background.getState(), {
    ...setup.background.getState(),
    blockedDomains: ["four.example"],
    lastGeneration: 4,
  });
});

test("disable, dry_run, empty domains, and invalid policy clear regardless of generation", () => {
  for (const disabled of [
    policy({generation: 1, enforce: false}),
    policy({generation: 1, dry_run: true}),
    policy({generation: 1, domains: []}),
    {...policy({generation: 1}), version: 2},
  ]) {
    const setup = harness();
    setup.background.connectNative();
    setup.ports[0].onMessage.emit(policy({generation: 9, domains: ["active.example"]}));
    setup.ports[0].onMessage.emit(disabled);
    assert.deepEqual(setup.background.getState().blockedDomains, []);
    assert.equal(setup.background.getState().lastGeneration, 9);
  }
});

test("same adopted generation recovers after fail-open without accepting changed content", () => {
  const setup = harness();
  setup.background.connectNative();
  const connectedPort = setup.ports[0];
  connectedPort.onMessage.emit(policy({generation: 9, domains: ["active.example"]}));
  connectedPort.onMessage.emit(policy({generation: 9, enforce: false, domains: []}));
  connectedPort.onMessage.emit(policy({generation: 9, domains: ["changed.example"]}));
  assert.deepEqual(setup.background.getState().blockedDomains, []);
  connectedPort.onMessage.emit(policy({generation: 9, domains: ["active.example"]}));
  assert.deepEqual(setup.background.getState().blockedDomains, ["active.example"]);
});

test("disconnect clears policy, resets watermark, and schedules one reconnect timer", () => {
  const setup = harness();
  setup.background.connectNative();
  setup.ports[0].onMessage.emit(policy({generation: 7}));
  setup.ports[0].onDisconnect.emit();
  setup.background.scheduleReconnect();
  assert.deepEqual(setup.background.getState().blockedDomains, []);
  assert.equal(setup.background.getState().lastGeneration, -1);
  assert.equal(setup.timers.length, 1);
  assert.equal(setup.timers[0].milliseconds, 1000);
  setup.timers[0].callback();
  assert.equal(setup.ports.length, 2);
  assert.deepEqual(setup.ports[1].sent, [{type: "hello", version: 1}]);
});

test("reconnect failure backs off from one second to a maximum of thirty seconds", () => {
  const timers = [];
  const browserApi = {
    runtime: {
      connectNative() {
        throw new Error("unavailable");
      },
      getURL: (path) => `moz-extension://extension-id/${path}`,
    },
    webRequest: {onBeforeRequest: event()},
  };
  const timerApi = {
    setTimeout(callback, milliseconds) {
      const timer = {callback, milliseconds};
      timers.push(timer);
      return timer;
    },
    clearTimeout() {},
  };
  const background = createBackground(browserApi, timerApi);
  background.connectNative();
  for (let index = 0; index < 6; index += 1) {
    timers[index].callback();
  }
  assert.deepEqual(timers.map((timer) => timer.milliseconds), [1000, 2000, 4000, 8000, 16000, 30000, 30000]);
});

test("host matching uses exact or dot-boundary subdomain matches", () => {
  const domains = new Set(["example.com"]);
  assert.equal(isBlockedHost("example.com", domains), true);
  assert.equal(isBlockedHost("sub.example.com", domains), true);
  assert.equal(isBlockedHost("evil-example.com", domains), false);
  assert.equal(isBlockedHost("example.com.evil.test", domains), false);
});

test("redirect is synchronous, main_frame-only, normal-window-only, and avoids extension URLs", () => {
  const setup = harness();
  setup.background.start();
  setup.ports[0].onMessage.emit(policy());
  assert.deepEqual(setup.background.onBeforeRequest({type: "main_frame", url: "https://sub.example.com/private?q=secret#part", incognito: false}), {
    redirectUrl: "moz-extension://extension-id/blocked.html?host=sub.example.com",
  });
  assert.deepEqual(setup.background.onBeforeRequest({type: "image", url: "https://example.com/image.png"}), {});
  assert.deepEqual(setup.background.onBeforeRequest({type: "main_frame", url: "https://example.com", incognito: true}), {});
  assert.deepEqual(setup.background.onBeforeRequest({type: "main_frame", url: "moz-extension://extension-id/blocked.html?host=example.com"}), {});
  assert.deepEqual(setup.requests.listeners[0]({type: "main_frame", url: "https://example.com"}), {
    redirectUrl: "moz-extension://extension-id/blocked.html?host=example.com",
  });
});

test("blocked page renders normalized host and fixed reason with textContent", () => {
  assert.equal(readBlockedHost("?host=Sub.Example.COM."), "sub.example.com");
  assert.equal(readBlockedHost("?host=https%3A%2F%2Fexample.com%2Fsecret"), "");
  const elements = {"blocked-host": {}, "blocked-reason": {}};
  const documentObject = {getElementById: (id) => elements[id]};
  renderBlockedHost(documentObject, "?host=%3Cimg%20src%3Dx%3E");
  assert.equal(elements["blocked-host"].textContent, "");
  assert.equal(elements["blocked-reason"].textContent, BlockedPageReason);
});
