// 使用原版 DoroPay 六字段 API 验证客户端映射；只运行独立本地 Miniflare D1。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import { before, after, test } from "node:test";

const workerRoot = new URL("../../../DoroPay/payment-worker/", import.meta.url);
const available = existsSync(fileURLToPath(new URL("node_modules/miniflare", workerRoot)));
const vectors = JSON.parse(await readFile(new URL("./fixtures/android-membership-vectors.json", import.meta.url), "utf8"));
const device = (name) => vectors.find((v) => v.name === name).expected;
let mf, db, ip = 1;
before(async () => {
  if (!available) return;
  const require = createRequire(new URL("package.json", workerRoot));
  const { Miniflare } = require("miniflare");
  mf = new Miniflare({ compatibilityDate: "2025-01-01", modules: true,
    modulesRoot: fileURLToPath(workerRoot),
    modulesRules: [{ type: "ESModule", include: ["**/*.js"] }],
    scriptPath: fileURLToPath(new URL("src/index.js", workerRoot)),
    d1Databases: { DB: "client-mapping-legacy-contract" },
    bindings: { INVITE_TOKEN_SECRET: "client-mapping-contract-test-secret-long-enough" },
  });
  db = await mf.getD1Database("DB");
  const schema = await readFile(new URL("db/schema.sql", workerRoot), "utf8");
  for (const statement of schema.replace(/--[^\n]*/g, "").split(";")) {
    if (statement.trim()) await db.prepare(statement).run();
  }
});
after(async () => { await mf?.dispose(); });
async function post(path, data) {
  const response = await mf.dispatchFetch("http://mapping.test" + path, {
    method: "POST", headers: { "Content-Type": "application/json", "CF-Connecting-IP": `198.51.100.${ip++}`, "User-Agent": "MDA/2.5.0-android-test.2" },
    body: JSON.stringify(data),
  });
  return { status: response.status, body: await response.json() };
}
function ok(value) { assert.equal(value.status, 200, JSON.stringify(value.body)); return value.body; }

test("original DoroPay accepts six mapped fields and tolerates App ID/fallback changes", { skip: !available }, async () => {
  const base = device("primary-fallback");
  assert.equal(Object.keys(base).length, 6);
  assert.ok(Object.values(base).every((v) => /^[a-f0-9]{64}$/.test(v)));
  const order = "3456789012345678901234567890";
  ok(await post("/api/register", { ...base, user_id: "PhoneAlpha", order_id: order, amount_orange: 100, original_amount: 100 }));
  ok(await post("/api/purchase", { ...base, user_id: "PhoneAlpha", plan_code: "orange_plus_monthly", months: 1 }));
  const matched = ok(await post("/api/members/v7/device-status", base));
  assert.equal(matched.is_member, true); assert.equal(matched.score, 100);
  for (const name of ["app-a", "app-b"]) {
    const status = ok(await post("/api/members/v7/device-status", device(name)));
    assert.equal(status.is_member, true); assert.equal(status.user_id, "PhoneAlpha"); assert.equal(status.score, 90);
  }
  assert.deepEqual(device("zero-app-id"), base);
  assert.equal(ok(await post("/api/members/v7/device-status", device("changed-sn"))).matched, false);
  assert.equal(ok(await post("/api/members/v7/device-status", device("changed-privileged-id"))).matched, false);
  const params = new URLSearchParams(base);
  const response = await mf.dispatchFetch("http://mapping.test/api/query/device?" + params);
  assert.equal((await response.json()).members[0].user_id, "PhoneAlpha");
});

test("original rebind migrates mapped phones without a protocol or schema change", { skip: !available }, async () => {
  const next = device("changed-sn");
  ok(await post("/api/device/rebind", { ...next, user_id: "PhoneAlpha", order_id: "3456789012345678901234567890" }));
  assert.equal(ok(await post("/api/members/v7/device-status", device("primary-fallback"))).matched, false);
  assert.equal(ok(await post("/api/members/v7/device-status", next)).is_member, true);
  const fields = await db.prepare("PRAGMA table_info(members)").all();
  assert.equal(fields.results.some((f) => f.name === "device_schema"), false);
});

test("plain Windows identities still work with a separate user ID", { skip: !available }, async () => {
  const pc = Object.fromEntries(["cpu", "uuid", "bios", "board", "disk", "guid"].map((f) => [f + "_hash", createHash("sha256").update("pc-fixture-" + f).digest("hex")]));
  ok(await post("/api/register", { ...pc, user_id: "DesktopAlpha", order_id: "4567890123456789012345678901", amount_orange: 30 }));
  const status = ok(await post("/api/members/v7/device-status", pc));
  assert.equal(status.matched, true); assert.equal(status.user_id, "DesktopAlpha"); assert.equal(status.score, 100);
});
