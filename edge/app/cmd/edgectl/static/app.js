"use strict";

// The board, scoped to one week, one book and one market at a time.
//
// A full week is 16 games x 7 books x 3 markets, and no phone screen makes
// 336 fields usable. Scoping to a book and a market turns it into 32 inputs in
// kickoff order, which is also the order the book's own page lists them, so
// entering a week is a straight run down two columns with no hunting.

// Every request is resolved against BASE rather than written as a root-
// relative path, because this page is served two ways.
//
// Locally it sits at "/". Over Tailscale it is mounted with
// `tailscale serve --set-path=/edge`, which strips the mount before
// forwarding -- so the backend still sees /api/board and needs no prefix
// awareness. The catch is on this side: a relative "api/board" resolves
// against the CURRENT path, so it is correct at /edge/ and wrong at /edge,
// where it escapes the mount and hits the tailnet root. Deriving the base and
// forcing the trailing slash is correct at "/", "/edge" and "/edge/" alike.
const BASE = location.pathname.endsWith("/") ? location.pathname : location.pathname + "/";

const MARKETS = ["ml", "spread", "total"];
const STORE = "edgectl.board";

// There is deliberately no automatic advance between fields.
//
// A pause-based one lived here and was removed: it could not distinguish "I
// have finished this number" from "I am reading the next one off the app", and
// it silently split 3.5 across two fields, putting ".5" into a price. A
// length-based rule is no better -- a price is three digits (+150) or four
// (+1000) and there is no way to tell which you are halfway through. Enter and
// Tab advance. Nothing else moves the cursor.

const el = {
  week: document.getElementById("week"),
  book: document.getElementById("book"),
  rows: document.getElementById("rows"),
  hint: document.getElementById("hint"),
  banner: document.getElementById("banner"),
  pasteToggle: document.getElementById("pasteToggle"),
  pasteBody: document.getElementById("pasteBody"),
  blob: document.getElementById("blob"),
  preview: document.getElementById("preview"),
  apply: document.getElementById("apply"),
  diff: document.getElementById("diff"),
};

const state = load();
let data = null;      // last /api/board payload
let inputs = [];      // every input in tab order, for auto-advance

// ---- selection, remembered ---------------------------------------------

// The selection is persisted because a week is entered over several sittings
// and a reload that dropped you back on week 1 / the first book would cost a
// scroll and a wrong-column entry every time.
function load() {
  let s = {};
  try { s = JSON.parse(localStorage.getItem(STORE) || "{}"); } catch (e) { s = {}; }
  return {
    week: Number(s.week) || 1,
    book: s.book || "fanatics",
  };
}

function save() {
  try { localStorage.setItem(STORE, JSON.stringify(state)); } catch (e) { /* private mode */ }
}

// ---- value shapes -------------------------------------------------------

// A stored cell is "away/home" for the moneyline and "line away/home" for a
// lined market. The page never asks anyone to type the slash or the space: it
// splits on read and joins on write.

function splitStored(market, s) {
  s = (s || "").trim();
  if (market === "ml") {
    if (!s) return ["", ""];
    const i = s.indexOf("/");
    if (i < 0) return [s, ""];
    return [s.slice(0, i).trim(), s.slice(i + 1).trim()];
  }
  if (!s) return ["", "", ""];
  const sp = s.indexOf(" ");
  if (sp < 0) return [s, "", ""];
  const line = s.slice(0, sp).trim();
  const rest = s.slice(sp + 1);
  const i = rest.indexOf("/");
  if (i < 0) return [line, rest.trim(), ""];
  return [line, rest.slice(0, i).trim(), rest.slice(i + 1).trim()];
}

function joinValue(market, vals) {
  if (market === "ml") {
    const [a, b] = vals;
    if (!a && !b) return { value: "" };
    if (!a || !b) return { partial: true };
    return { value: a + "/" + b };
  }
  let [line, a, b] = vals;
  if (!line && !a && !b) return { value: "" };
  if (!line) return { partial: true };
  // Standard juice fills itself in. A lined market is -110/-110 unless the
  // book says otherwise, and typing that thirty-two times a week is exactly
  // the friction this page exists to remove. The filled values are written
  // back into the boxes so nothing is saved that you cannot see.
  const filled = [line, a || "-110", b || "-110"];
  return { value: filled[0] + " " + filled[1] + "/" + filled[2], filled: filled };
}

function fmtCons(market, s) {
  return s ? s : "";
}

// ---- rendering ----------------------------------------------------------

function fieldHTML(kind, value, placeholder) {
  // kind: "price" (sign chip, defaults to -) | "spread" (sign chip, defaults
  // to +) | "total" (no chip; a total is never negative).
  const sign = value ? (value.trim()[0] === "-" ? "-" : "+")
    : (kind === "price" ? "-" : "+");
  const digits = (value || "").replace(/[^0-9.]/g, "");
  const numeric = kind === "price" ? "numeric" : "decimal";
  const hidden = kind === "total" ? " hidden" : "";
  return `<div class="field">
    <button type="button" class="sign${hidden}" data-sign="${sign}">${sign === "-" ? "−" : "+"}</button>
    <input inputmode="${numeric}" enterkeyhint="next" autocomplete="off"
           spellcheck="false" value="${digits}" placeholder="${placeholder}">
  </div>`;
}

function kickoffLabel(k) {
  // Kickoffs are stored as local wall-clock strings, not instants; showing the
  // day and time verbatim avoids inventing a timezone conversion.
  if (!k) return "";
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(k);
  if (!m) return k;
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  const day = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"][d.getDay()];
  return `${day} ${m[4]}:${m[5]}`;
}

function render() {
  el.rows.innerHTML = "";
  inputs = [];
  if (!data) return;

  el.hint.textContent =
    "All three markets per game. Digits only — tap −/+ to flip a side. " +
    "Lines take a decimal (3.5). Leave the two prices blank for −110/−110. " +
    "Enter or Tab moves on; each row saves when you leave it.";

  for (const g of data.games) {
    const lines = g.books[state.book] || {};
    const consBook = g.books.consensus || {};

    const card = document.createElement("div");
    card.className = "card";
    card.innerHTML = `
      <div class="head">
        <span class="teams">${g.away} @ ${g.home}</span>
        <span class="kick">${kickoffLabel(g.kickoff)}</span>
      </div>`;

    // One .row per market, each its own save unit. Splitting by market rather
    // than by game keeps a bad spread from blocking a good moneyline: the two
    // are separate cells on disk and separate POSTs, so one can be rejected
    // while the other is already saved.
    for (const market of MARKETS) {
      const stored = lines[market] || "";
      const vals = splitStored(market, stored);

      const row = document.createElement("div");
      row.className = "row";
      row.dataset.game = g.id;
      row.dataset.market = market;

      const cons = state.book === "consensus" ? "" : fmtCons(market, consBook[market]);
      const fields = market === "ml"
        ? fieldHTML("price", vals[0], g.away) + fieldHTML("price", vals[1], g.home)
        : fieldHTML(market === "spread" ? "spread" : "total", vals[0], market === "spread" ? "line" : "total")
        + fieldHTML("price", vals[1], market === "total" ? "over" : g.away)
        + fieldHTML("price", vals[2], market === "total" ? "under" : g.home);

      row.innerHTML = `
        <div class="mkt">${market === "ml" ? "ML" : market}</div>
        <div class="fields">${fields}</div>
        <span class="cons">${cons}</span>
        <span class="dot"></span>
        <div class="err-msg" hidden></div>`;

      row.dataset.saved = stored;
      card.appendChild(row);
      for (const inp of row.querySelectorAll("input")) inputs.push(inp);
    }
    el.rows.appendChild(card);
  }
}

// ---- reading a row ------------------------------------------------------

function readRow(row) {
  const out = [];
  for (const f of row.querySelectorAll(".field")) {
    const digits = f.querySelector("input").value.trim();
    if (!digits) { out.push(""); continue; }
    const btn = f.querySelector(".sign");
    const sign = btn.classList.contains("hidden") ? "" : btn.dataset.sign;
    out.push(sign === "+" ? "+" + digits : sign + digits);
  }
  return out;
}

function writeRow(row, vals) {
  const fields = row.querySelectorAll(".field");
  vals.forEach((v, i) => {
    const f = fields[i];
    if (!f) return;
    f.querySelector("input").value = (v || "").replace(/[^0-9.]/g, "");
    const btn = f.querySelector(".sign");
    if (v) setSign(btn, v.trim()[0] === "-" ? "-" : "+");
  });
}

function setSign(btn, sign) {
  btn.dataset.sign = sign;
  btn.textContent = sign === "-" ? "−" : "+";
}

function mark(row, cls, msg) {
  const dot = row.querySelector(".dot");
  dot.className = "dot" + (cls ? " " + cls : "");
  const err = row.querySelector(".err-msg");
  err.textContent = msg || "";
  err.hidden = !msg;
  for (const f of row.querySelectorAll(".field")) f.classList.toggle("err", cls === "error");
}

// saveRow posts one cell, or does nothing if the row has not changed.
//
// There is no submit button anywhere on this page, on purpose: a session that
// loses ten minutes of typing to a backgrounded tab is the failure this is
// built to avoid, and the only defence that always works is to never be
// holding unsaved work.
async function saveRow(row) {
  const market = row.dataset.market;
  const joined = joinValue(market, readRow(row));
  if (joined.partial) { mark(row, "partial", ""); return; }
  if (joined.filled) writeRow(row, joined.filled);
  if (joined.value === row.dataset.saved) { mark(row, joined.value ? "saved" : "", ""); return; }

  mark(row, "saving", "");
  try {
    const res = await fetch(BASE + "api/price", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        week: state.week, game_id: row.dataset.game,
        book: state.book, market: market, value: joined.value,
      }),
    });
    const body = await res.json();
    if (!res.ok) {
      // The rejected text stays in the box. A value that vanished with an
      // error would leave you unable to see what you had mistyped.
      mark(row, "error", body.error || ("HTTP " + res.status));
      return;
    }
    row.dataset.saved = body.value || "";
    // Show exactly what landed on disk, sign and all.
    writeRow(row, splitStored(market, body.value || ""));
    mark(row, body.value ? "saved" : "", "");
  } catch (e) {
    mark(row, "error", "not saved: " + e.message);
  }
}

// ---- input behaviour ----------------------------------------------------


function advanceFrom(input) {
  const i = inputs.indexOf(input);
  if (i >= 0 && i + 1 < inputs.length) inputs[i + 1].focus();
  else input.blur(); // last field: close the keypad rather than trap it
}

el.rows.addEventListener("input", (e) => {
  const input = e.target;
  if (input.tagName !== "INPUT") return;
  // Digits (and a decimal point for a line) only; the sign lives on the chip.
  const cleaned = input.value.replace(/[^0-9.]/g, "");
  if (cleaned !== input.value) input.value = cleaned;

  // No timed auto-advance. It was not merely disliked, it corrupted input:
  // a spread is 3.5, and typing "3" then pausing to read the next number moved
  // focus to the odds field, so ".5" landed there and the row was rejected as
  // "not an integer". Any timeout long enough to be safe is long enough to be
  // useless, because there is no way to know whether a pause means "done" or
  // "reading". Enter and Tab advance; nothing else does.
});

el.rows.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && e.target.tagName === "INPUT") {
    e.preventDefault();
    advanceFrom(e.target);
  }
});

// focusout rather than blur: blur does not bubble, and one listener on the
// list is what keeps this working after a re-render.
el.rows.addEventListener("focusout", (e) => {
  const row = e.target.closest && e.target.closest(".row");
  if (!row) return;
  // Moving between two fields of the same row is not a finished edit.
  if (row.contains(e.relatedTarget)) return;
  saveRow(row);
});

el.rows.addEventListener("click", (e) => {
  const btn = e.target.closest(".sign");
  if (!btn) return;
  setSign(btn, btn.dataset.sign === "-" ? "+" : "-");
  const row = btn.closest(".row");
  // Flipping a sign is a finished edit on its own: it is the whole difference
  // between a favourite and a dog.
  if (!row.contains(document.activeElement)) saveRow(row);
});

// ---- selectors ----------------------------------------------------------

el.week.addEventListener("change", () => {
  state.week = Number(el.week.value); save(); refresh();
});
el.book.addEventListener("change", () => {
  state.book = el.book.value; save(); render();
});


function fillSelect(sel, values, current, label) {
  sel.innerHTML = "";
  for (const v of values) {
    const o = document.createElement("option");
    o.value = v;
    o.textContent = label ? label(v) : v;
    sel.appendChild(o);
  }
  sel.value = current;
  if (sel.value !== String(current) && values.length) sel.value = values[0];
  return sel.value;
}

// ---- paste import (desktop) --------------------------------------------

el.pasteToggle.addEventListener("click", () => {
  el.pasteBody.hidden = !el.pasteBody.hidden;
});

async function importCall(path) {
  const res = await fetch(BASE + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ week: state.week, book: state.book, market: "ml", blob: el.blob.value }),
  });
  const body = await res.json();
  if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
  return body;
}

el.preview.addEventListener("click", async () => {
  el.apply.disabled = true;
  try {
    const body = await importCall("api/import/preview");
    if (!body.changes || !body.changes.length) {
      el.diff.textContent = "nothing to change: these prices are already on the board.";
      return;
    }
    el.diff.innerHTML = body.changes.map((c) =>
      `<div class="add">${c.game_id}  ${c.away} @ ${c.home}  ${c.old || "(empty)"} → ${c.new}</div>`
    ).join("");
    // Confirmation is not optional: a blob that shifted by one entry produces
    // a diff that is entirely plausible until you read the team names.
    el.apply.disabled = false;
  } catch (e) {
    el.diff.innerHTML = `<div class="bad">${e.message}</div>`;
  }
});

el.apply.addEventListener("click", async () => {
  el.apply.disabled = true;
  try {
    const body = await importCall("api/import/apply");
    el.diff.innerHTML = `<div class="add">wrote ${body.applied} price(s).</div>`;
    el.blob.value = "";
    refresh();
  } catch (e) {
    el.diff.innerHTML = `<div class="bad">${e.message}</div>`;
  }
});

// ---- load ---------------------------------------------------------------

async function refresh(retried) {
  try {
    const res = await fetch(BASE + "api/board?week=" + encodeURIComponent(state.week));
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
    data = body;
    el.banner.hidden = true;

    state.week = fillSelect(el.week, data.weeks, data.week, (w) => "Week " + w) * 1;
    // consensus is a generated reference column, not a book you can bet or
    // edit, so it is never offered as a target.
    const books = data.books.filter((b) => b !== "consensus");
    state.book = fillSelect(el.book, books, state.book, (b) => b);
        save();
    render();
  } catch (e) {
    // A remembered week whose file has since been removed would otherwise
    // leave the page with no week selector to escape from. Fall back once.
    if (!retried && state.week !== 1) {
      state.week = 1;
      save();
      return refresh(true);
    }
    el.banner.hidden = false;
    el.banner.textContent = "could not load the board: " + e.message;
  }
}

refresh();
