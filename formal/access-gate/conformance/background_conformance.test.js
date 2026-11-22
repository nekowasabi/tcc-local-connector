"use strict";
// AccessGate.tla の拡張側の定義が firefox-extension/background.js と一致することを確かめる適合テスト。
// ここが落ちたら、拡張がモデルの「今のコード」の列から動いた合図（.claude/skills/verify-access-gate/SKILL.md）。

const assert = require("node:assert/strict");
const test = require("node:test");
const path = require("node:path");
const {createBackground} = require(path.join(__dirname, "..", "..", "..", "firefox-extension", "background.js"));

function event() {
  const listeners = [];
  return {addListener(listener) { listeners.push(listener); }, emit(value) { for (const l of listeners) l(value); }};
}

function harness() {
  const ports = [];
  const updates = [];
  const api = {
    runtime: {
      connectNative() {
        const p = {onMessage: event(), onDisconnect: event(), postMessage() {}};
        ports.push(p);
        return p;
      },
      getURL(p) { return "moz-extension://id/" + p; },
    },
    webRequest: {onBeforeRequest: event()},
    tabs: {
      async query() { return [{id: 1, url: "https://x.com/home"}, {id: 2, url: "https://example.org/"}]; },
      async update(id, value) { updates.push([id, value]); },
    },
  };
  const timerApi = {setTimeout() { return {}; }, clearTimeout() {}};
  const background = createBackground(api, timerApi);
  background.start();
  const adopt = () => background.handleNativeMessage({type: "policy", version: 1, generation: 1, enforce: true, dry_run: false, domains: ["x.com"]});
  const load = (extra = {}) => background.onBeforeRequest({type: "main_frame", url: "https://x.com/home", ...extra});
  return {background, ports, updates, adopt, load};
}

test("ExtSync: 採用したポリシーは通常ウィンドウの新しい読み込みを止める（Blocked(\"ff\")）", () => {
  const h = harness();
  h.adopt();
  assert.match(h.load().redirectUrl ?? "", /blocked\.html\?host=x\.com/);
});

test("privateBlocked: プライベートウィンドウの読み込みも止める", () => {
  const h = harness();
  h.adopt();
  assert.match(h.load({incognito: true}).redirectUrl ?? "", /blocked\.html\?host=x\.com/);
});

test("closeOpenTabs: ポリシー採用時に開いている対象タブを遮断画面へ移す", async () => {
  const h = harness();
  await h.adopt();
  assert.deepEqual(h.updates, [[1, {url: "moz-extension://id/blocked.html?host=x.com"}]]);
});

test("OwnerQuit: native host が切れると遮断を消す", () => {
  const h = harness();
  h.adopt();
  h.ports[0].onDisconnect.emit();
  assert.deepEqual(h.load(), {});
});

test("OwnerQuit: heartbeat が切れた native host の空ポリシーで遮断を消す", () => {
  const h = harness();
  h.adopt();
  h.ports[0].onMessage.emit({type: "policy", version: 1, generation: 1, enforce: false, dry_run: false, domains: []});
  assert.deepEqual(h.load(), {});
});
