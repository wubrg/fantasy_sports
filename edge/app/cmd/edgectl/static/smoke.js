// A headless smoke test for app.js. Run with: node smoke.js
//
// This exists because `node --check` passes on code that cannot run. It only
// parses; a function deleted by an over-broad edit is a ReferenceError at
// render time, and the page still answers HTTP 200 while showing nothing but
// "could not load the board". That failure shipped twice. Serving the file is
// not evidence that it works -- executing it is.
//
// The DOM stub is deliberately shallow: just enough to let app.js reach
// render() and build rows. It is not a browser and does not pretend to be. It
// catches "this identifier does not exist" and "this path throws", which is
// the class of bug that got through.

"use strict";
const fs = require("fs");
const path = require("path");
const vm = require("vm");

let failures = 0;
function fail(msg) { failures++; console.error("  FAIL " + msg); }
function ok(msg) { console.log("  ok   " + msg); }

// ---- the smallest DOM that app.js can run against -----------------------

function makeEl(tag) {
  const el = {
    tagName: (tag || "div").toUpperCase(),
    children: [], dataset: {}, style: {}, attrs: {},
    className: "", value: "", textContent: "", hidden: false,
    classList: {
      _s: new Set(),
      add(c) { this._s.add(c); }, remove(c) { this._s.delete(c); },
      toggle(c, on) { on ? this._s.add(c) : this._s.delete(c); },
      contains(c) { return this._s.has(c); },
    },
    appendChild(c) { this.children.push(c); return c; },
    addEventListener() {}, removeEventListener() {},
    setAttribute(k, v) { this.attrs[k] = v; }, getAttribute(k) { return this.attrs[k]; },
    focus() {}, blur() {}, closest() { return null; },
    querySelector(sel) { return this.querySelectorAll(sel)[0] || null; },
    querySelectorAll(sel) { return collect(this, sel); },
    get innerHTML() { return this._html || ""; },
    // Parsing HTML is out of scope, but the field/input structure has to be
    // discoverable or readRow and writeRow cannot be exercised at all.
    set innerHTML(h) { this._html = h; this.children = parseFields(h); },
  };
  return el;
}

// parseFields finds .field blocks and their inputs in a template string. It is
// a crude scan, not a parser, and only understands the shapes app.js emits.
function parseFields(html) {
  const out = [];
  const re = /<div class="field">([\s\S]*?)<\/div>\s*<\/div>/g;
  let m;
  while ((m = re.exec(html)) !== null) {
    const field = makeEl("div");
    field.className = "field";
    const input = makeEl("input");
    const val = /value="([^"]*)"/.exec(m[1]);
    input.value = val ? val[1] : "";
    field.appendChild(input);
    out.push(field);
  }
  return out;
}

function collect(root, sel) {
  const want = sel.replace(/^\./, "");
  const out = [];
  (function walk(n) {
    for (const c of n.children || []) {
      const isInput = sel === "input" && c.tagName === "INPUT";
      const isClass = sel.startsWith(".") && c.className && c.className.split(" ").includes(want);
      if (isInput || isClass) out.push(c);
      walk(c);
    }
  })(root);
  return out;
}

const ids = {};
const document = {
  getElementById(id) { return (ids[id] = ids[id] || makeEl("div")); },
  createElement: makeEl,
  addEventListener() {},
};
const localStorage = {
  _d: {},
  getItem(k) { return this._d[k] || null; },
  setItem(k, v) { this._d[k] = String(v); },
};
const location = { pathname: "/edge/", search: "", hash: "" };

const sandbox = {
  document, localStorage, location, console,
  fetch: async () => ({ ok: true, json: async () => ({}) }),
  setTimeout, clearTimeout, JSON, Number, String, Math, Object, Array, Date, RegExp, isNaN,
};
sandbox.window = sandbox;

// ---- load ----------------------------------------------------------------

const src = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
const ctx = vm.createContext(sandbox);
try {
  vm.runInContext(src, ctx, { filename: "app.js" });
  ok("app.js evaluates");
} catch (e) {
  fail("app.js threw on load: " + e.message);
  process.exit(1);
}

// ---- exercise the paths that broke --------------------------------------

const sample = {
  games: [{
    id: "2026_01_NE_SEA", away: "NE", home: "SEA", kickoff: "2026-09-09T20:20",
    books: {
      fanatics: { ml: "+200/-165", spread: "+3.5 -110/-110", total: "44.5 -110/-110" },
      consensus: { ml: "+160/-192", spread: "3.5 -110/-110", total: "44.5 -110/-110" },
    },
  }],
};

// `data` and `state` are let/const, so they are NOT properties of the sandbox
// -- assigning ctx.data would create a shadow the script never reads, and the
// test would pass while exercising nothing. Assign inside the context instead.
function inCtx(code) { return vm.runInContext(code, ctx, { filename: "smoke" }); }

sandbox.__sample = sample;
function tryRender(what, setup) {
  try {
    inCtx(setup + "; render(); el.rows.children.length");
    const n = inCtx("el.rows.children.length");
    if (n > 0) ok(`${what} (${n} card(s) built)`);
    else fail(`${what}: render produced no rows`);
  } catch (e) {
    fail(`${what}: ${e.message}`);
  }
}

tryRender("render() against a priced game", 'data = __sample; state.book = "fanatics"');
tryRender("render() with consensus selected", 'data = __sample; state.book = "consensus"');
tryRender("render() against a game with no prices",
  'data = { games: [{ id: "x", away: "A", home: "B", kickoff: "2026-09-09T20:20", books: {} }] }; state.book = "fanatics"');

// ---- the bets view ------------------------------------------------------
// Same reasoning as render(): this path is reachable only by tapping a tab, so
// a ReferenceError in it would ship unnoticed for as long as nobody tapped.

const sampleReport = {
  week: 1, book: "fanatics", priced: 16, total: 16, target: 0.7, floor: 234,
  suspect: [{ game: "NE @ SEA", price: "-165/+200", overround: -0.044, why: "no book posts a market this thin" }],
  dogs: [
    { team: "ARI", game: "g1", price: 455, fair: 0.173, conversion: 0.787, clears: true, suspect: false },
    { team: "NO", game: "g2", price: 250, fair: 0.274, conversion: 0.686, clears: false, suspect: false },
    { team: "SEA", game: "g3", price: 200, fair: 0.349, conversion: 0.697, clears: false, suspect: true },
  ],
  set: [{ teams: ["WAS", "GB"], price: 494, true_prob: 0.154, conversion: 0.762 }],
  avg_conversion: 0.762, any_hit: 0.154, unfilled: 3,
  shop: [{ team: "ARI", best: 390, book: "fanatics", cons: 455, points: -65, points_valid: true }],
  notes: [],
};

function tryReport(what, payload) {
  try {
    sandbox.__rep = payload;
    inCtx("renderReport(__rep)");
    const html = inCtx("el.report.innerHTML");
    if (html && html.length > 0) ok(`${what}`);
    else fail(`${what}: produced nothing`);
  } catch (e) {
    fail(`${what}: ${e.message}`);
  }
}

tryReport("renderReport() on a full report", sampleReport);
tryReport("renderReport() with nothing priced",
  { week: 1, book: "fanatics", priced: 0, total: 16, target: 0.7, floor: 234,
    suspect: [], dogs: [], set: [], avg_conversion: 0, any_hit: 0, unfilled: 4, shop: [], notes: [] });
tryReport("renderReport() with prices but no buildable set",
  Object.assign({}, sampleReport, { set: [], suspect: [], shop: [], notes: [] }));
tryReport("renderReport() with null collections (Go emits null, not [])",
  { week: 1, book: "fanatics", priced: 2, total: 16, target: 0.7, floor: 234,
    suspect: null, dogs: [], set: null, avg_conversion: 0, any_hit: 0, unfilled: 4,
    shop: null, notes: null });

try {
  inCtx('state.view = "bets"; syncView(); 0');
  ok("syncView() switches to the bets view");
  inCtx('state.view = "enter"; syncView(); 0');
} catch (e) {
  fail("syncView() threw: " + e.message);
}

// ---- value model --------------------------------------------------------

function eq(what, got, want) {
  if (got === want) ok(what);
  else fail(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

eq("splitStored ml", JSON.stringify(ctx.splitStored("ml", "+200/-165")), '["+200","-165"]');
eq("splitStored spread mirrors the far side",
  JSON.stringify(ctx.splitStored("spread", "+3.5 -110/-110")), '["+3.5","-110","-3.5","-110"]');
eq("splitStored total repeats the line",
  JSON.stringify(ctx.splitStored("total", "44.5 -110/-115")), '["+44.5","-110","+44.5","-115"]');

eq("joinValue ml", ctx.joinValue("ml", ["+200", "-165"]).value, "+200/-165");
eq("joinValue spread from one side only",
  ctx.joinValue("spread", ["+3.5", "", "", ""]).value, "+3.5 -110/-110");
eq("joinValue spread keeps typed juice",
  ctx.joinValue("spread", ["+3.5", "-105", "-3.5", "-115"]).value, "+3.5 -105/-115");
// The decimal that started all this: it must survive as one value.
eq("joinValue keeps 3.5 intact",
  ctx.joinValue("spread", ["-3.5", "", "", ""]).value, "-3.5 -110/-110");
eq("joinValue flags a mirror conflict",
  !!ctx.joinValue("spread", ["+3.5", "", "+4.5", ""]).conflict, true);
eq("joinValue flags a total disagreeing with itself",
  !!ctx.joinValue("total", ["44.5", "", "45.5", ""]).conflict, true);
eq("joinValue empty stays empty", ctx.joinValue("spread", ["", "", "", ""]).value, "");
eq("joinValue partial ml", !!ctx.joinValue("ml", ["+200", ""]).partial, true);

console.log(failures ? `\n${failures} failure(s)` : "\nall smoke checks passed");
process.exit(failures ? 1 : 0);
