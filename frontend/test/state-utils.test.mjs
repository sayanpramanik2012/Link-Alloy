import assert from "node:assert/strict";
import test from "node:test";
import { appendRollingSample, errorReducer, isStale, pollInterval, withTimeout } from "../.test-dist/state-utils.js";

test("background poll results do not erase a persistent tunnel action error", () => {
  let state = { actionError: "Route install failed", refreshError: "" };
  state = errorReducer(state, { type: "poll-result", message: "" });
  assert.equal(state.actionError, "Route install failed");
  assert.equal(state.refreshError, "");
  state = errorReducer(state, { type: "poll-result", message: "Status timed out" });
  assert.equal(state.actionError, "Route install failed");
  assert.equal(state.refreshError, "Status timed out");
  state = errorReducer(state, { type: "action-dismissed" });
  assert.equal(state.actionError, "");
});

test("stale-state threshold treats unknown and recent status as fresh", () => {
  assert.equal(isStale(null, 20_000), false);
  assert.equal(isStale(15_000, 20_000), false);
  assert.equal(isStale(7_999, 20_000), true);
});

test("polling is faster only while the tunnel is active", () => {
  assert.equal(pollInterval("running"), 1000);
  assert.equal(pollInterval("stopped"), 4000);
  assert.equal(pollInterval("error"), 4000);
});

test("throughput history is bounded by age and point count", () => {
  const old = [{ at: 1, value: 1 }, { at: 59_000, value: 2 }];
  const result = appendRollingSample(old, { at: 61_000, value: 3 }, 60_000, 2);
  assert.deepEqual(result, [{ at: 59_000, value: 2 }, { at: 61_000, value: 3 }]);
});

test("request timeout rejects and cancels a pending Wails call", async () => {
  let cancelled = false;
  const pending = new Promise(() => {});
  pending.cancel = () => { cancelled = true; };
  await assert.rejects(withTimeout(pending, 5, "Status refresh"), /Status refresh timed out/);
  assert.equal(cancelled, true);
});

test("request timeout leaves a successful request intact", async () => {
  const value = await withTimeout(Promise.resolve("ready"), 100, "Status refresh");
  assert.equal(value, "ready");
});
