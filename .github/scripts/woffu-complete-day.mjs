#!/usr/bin/env node
/**
 * Woffu - Auto Complete Day
 *
 * Fills today's presence slots via the Woffu API for non-holiday weekdays.
 *
 * Schedule generated:
 *   Mon-Thu (8h total):
 *     - Morning slot: entry 08:30-09:00, exit (lunch) 13:00-14:00
 *     - Afternoon slot: entry 14:30-15:00, exit 18:30-19:00
 *     (Entry is constrained to 08:30+ because the math of 4 ranges plus
 *      exactly 8h work only admits entry from 08:30 onwards. See README/PR.)
 *
 *   Fri (6h total):
 *     - Single slot: entry 08:00-09:00, exit = entry + 6h
 *
 * Env vars:
 *   WOFFU_TOKEN     JWT bearer token (required)
 *   WOFFU_USER_ID   Numeric user id (required)
 *   WOFFU_BASE_URL  Default: https://app.woffu.com
 *
 * Flags:
 *   --dry-run   Compute slots and print them, but don't call the API.
 */

const TOKEN = process.env.WOFFU_TOKEN || "";
const USER_ID = process.env.WOFFU_USER_ID || "";
const BASE_URL = (process.env.WOFFU_BASE_URL || "https://app.woffu.com").replace(/\/+$/, "");
const DRY_RUN = process.argv.includes("--dry-run");

if (!TOKEN || !USER_ID) {
  console.error("ERROR: WOFFU_TOKEN and WOFFU_USER_ID must be set");
  process.exit(1);
}

const HEADERS = {
  Authorization: `Bearer ${TOKEN}`,
  "Content-Type": "application/json",
  Accept: "application/json",
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function randInt(min, max) {
  return min + Math.floor(Math.random() * (max - min + 1));
}

function pad2(n) {
  return String(n).padStart(2, "0");
}

function minToHHMM(m) {
  return `${pad2(Math.floor(m / 60))}:${pad2(m % 60)}`;
}

/** Returns today in Europe/Madrid as { date: 'YYYY-MM-DD', dow: 1..7 } (1=Mon..7=Sun). */
function getTodayMadrid() {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Europe/Madrid",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    weekday: "short",
  }).formatToParts(new Date());

  const get = (type) => parts.find((p) => p.type === type).value;
  const weekday = get("weekday");
  const dowMap = { Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6, Sun: 7 };
  return {
    date: `${get("year")}-${get("month")}-${get("day")}`,
    dow: dowMap[weekday],
  };
}

// ---------------------------------------------------------------------------
// Woffu API
// ---------------------------------------------------------------------------

/**
 * Returns true when `date` is a working day for this user (expected hours > 0).
 * Holidays, weekends and full-day absences yield ExpectedSeconds === 0.
 */
async function isWorkingDay(date) {
  const url =
    `${BASE_URL}/api/svc/core/diariesquery/users/${USER_ID}` +
    `/diaries/summary/presence?userId=${USER_ID}` +
    `&fromDate=${date}&toDate=${date}` +
    `&pageSize=1&includeHourTypes=true&includeNotHourTypes=true&includeDifference=true`;

  const res = await fetch(url, { headers: HEADERS });
  if (!res.ok) {
    throw new Error(`diary summary failed: ${res.status} ${await res.text()}`);
  }
  const data = await res.json();
  const diaries = data?.Diaries ?? data?.diaries ?? [];
  const today = diaries.find((d) => (d?.Date ?? d?.date ?? "").startsWith(date));
  if (!today) {
    // No diary for today: treat as non-working to avoid stamping holidays.
    return { working: false, reason: "no diary entry returned" };
  }
  const expected = today.ExpectedSeconds ?? today.expectedSeconds ?? 0;
  const dayType = today.DayTypeName ?? today.dayTypeName ?? "";
  if (expected <= 0) {
    return { working: false, reason: `expected=${expected} dayType="${dayType}"` };
  }
  return { working: true, expected, dayType };
}

/** Returns true if there are already signs/slots logged for `date`. */
async function hasExistingSlots(date) {
  const url =
    `${BASE_URL}/api/svc/core/diariesquery/users/${USER_ID}` +
    `/diaries/summary/presence?userId=${USER_ID}` +
    `&fromDate=${date}&toDate=${date}` +
    `&pageSize=1&includeHourTypes=true&includeNotHourTypes=true&includeDifference=true`;
  const res = await fetch(url, { headers: HEADERS });
  if (!res.ok) return false;
  const data = await res.json();
  const diaries = data?.Diaries ?? data?.diaries ?? [];
  const today = diaries.find((d) => (d?.Date ?? d?.date ?? "").startsWith(date));
  if (!today) return false;
  const worked = today.WorkedSeconds ?? today.workedSeconds ?? 0;
  return worked > 0;
}

// ---------------------------------------------------------------------------
// Slot generation
// ---------------------------------------------------------------------------

/**
 * Mon-Thu: 2 slots, total work = 480 min (8h).
 *  - entry        ∈ [08:30, 09:00]  (510-540)
 *  - lunch_out    ∈ [13:00, 14:00]  (780-840)
 *  - lunch_in     ∈ [14:30, 15:00]  (870-900)
 *  - end          ∈ [18:30, 19:00]  (1110-1140)
 *  - (lunch_out - entry) + (end - lunch_in) === 480
 */
function pickMonThuSlots() {
  const entry = randInt(510, 540);

  // lunch_break ∈ [max(30, 630-entry), min(120, 660-entry)]
  const lbMin = Math.max(30, 630 - entry);
  const lbMax = Math.min(120, 660 - entry);
  const lunchBreak = randInt(lbMin, lbMax);

  const end = entry + 480 + lunchBreak; // guaranteed ∈ [1110, 1140]

  // lunch_out ∈ [max(780, 870 - lb), min(840, 900 - lb)]
  const loMin = Math.max(780, 870 - lunchBreak);
  const loMax = Math.min(840, 900 - lunchBreak);
  const lunchOut = randInt(loMin, loMax);
  const lunchIn = lunchOut + lunchBreak;

  return [
    { in_time: minToHHMM(entry), out_time: minToHHMM(lunchOut) },
    { in_time: minToHHMM(lunchIn), out_time: minToHHMM(end) },
  ];
}

/**
 * Fri: 1 slot, total work = 360 min (6h).
 *  - entry ∈ [08:00, 09:00]
 *  - exit  = entry + 6h  ∈ [14:00, 15:00]
 */
function pickFridaySlots() {
  const entry = randInt(480, 540);
  const exit = entry + 360;
  return [{ in_time: minToHHMM(entry), out_time: minToHHMM(exit) }];
}

// ---------------------------------------------------------------------------
// Payload (matches the MCP "complete_day" shape)
// ---------------------------------------------------------------------------

function buildPayload(date, slots) {
  const userId = parseInt(USER_ID, 10);
  const ts = Date.now();

  const formatted = slots.map((slot, i) => {
    const [inH, inM] = slot.in_time.split(":").map((n) => parseInt(n, 10));
    const [outH, outM] = slot.out_time.split(":").map((n) => parseInt(n, 10));
    const totalMin = outH * 60 + outM - (inH * 60 + inM);

    const inHHmm = `${pad2(inH)}:${pad2(inM)}:00`;
    const outHHmm = `${pad2(outH)}:${pad2(outM)}:00`;

    return {
      id: `${ts}-${i}`,
      in: {
        signId: 0,
        userId,
        date: `${date}T${pad2(inH)}:00:00.000Z`,
        trueDate: `${date}T${pad2(inH)}:00:00.000Z`,
        signIn: true,
        time: inHHmm,
        valueTime: inHHmm,
        shortTime: inHHmm,
        shortTrueTime: inHHmm,
        shortValueTime: inHHmm,
        utcTime: `${inHHmm} +0`,
        signType: 3,
        signStatus: 1,
        deviceType: 0,
        deleted: false,
      },
      out: {
        signId: 0,
        userId,
        date: `${date}T${pad2(outH)}:00:00.000Z`,
        trueDate: `${date}T${pad2(outH)}:00:00.000Z`,
        signIn: false,
        time: outHHmm,
        valueTime: outHHmm,
        shortTime: outHHmm,
        shortTrueTime: outHHmm,
        shortValueTime: outHHmm,
        utcTime: `${outHHmm} +0`,
        signType: 3,
        signStatus: 1,
        deviceType: 0,
        deleted: false,
      },
      totalMin,
    };
  });

  return { date, comments: "", userId, slots: formatted };
}

async function completeDay(date, slots) {
  const url = `${BASE_URL}/api/svc/core/users/${USER_ID}/diarysummaries/workday/slots/self`;
  const payload = buildPayload(date, slots);

  const res = await fetch(url, {
    method: "PUT",
    headers: HEADERS,
    body: JSON.stringify(payload),
  });

  if (!res.ok) {
    const text = await res.text();
    throw new Error(`complete_day failed: HTTP ${res.status} — ${text}`);
  }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

const { date, dow } = getTodayMadrid();
console.log(`Date (Madrid): ${date}  dow=${dow}  dry_run=${DRY_RUN}`);

if (dow < 1 || dow > 5) {
  console.log("Weekend — skipping.");
  process.exit(0);
}

const status = await isWorkingDay(date);
if (!status.working) {
  console.log(`Not a working day (${status.reason}) — skipping.`);
  process.exit(0);
}
console.log(`Working day confirmed (expected=${status.expected}s, dayType="${status.dayType}").`);

if (await hasExistingSlots(date)) {
  console.log("Day already has worked hours logged — skipping to avoid overwrite.");
  process.exit(0);
}

const slots = dow === 5 ? pickFridaySlots() : pickMonThuSlots();
const totalMin = slots.reduce((acc, s) => {
  const [ih, im] = s.in_time.split(":").map(Number);
  const [oh, om] = s.out_time.split(":").map(Number);
  return acc + (oh * 60 + om - (ih * 60 + im));
}, 0);

console.log("Generated slots:");
for (const s of slots) console.log(`  ${s.in_time}  →  ${s.out_time}`);
console.log(`Total: ${Math.floor(totalMin / 60)}h ${pad2(totalMin % 60)}m`);

if (DRY_RUN) {
  console.log("DRY RUN — payload not sent.");
  process.exit(0);
}

await completeDay(date, slots);
console.log("Done — day completed in Woffu.");
