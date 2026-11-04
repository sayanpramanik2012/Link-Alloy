import { test } from "node:test";
import assert from "node:assert/strict";
import { formatRate } from "../.test-dist/format.js";

test("Mbps uses decimal bits while MiB/s uses binary bytes", () => {
  assert.equal(formatRate(12_500_000, "bits"), "100 Mbps");
  assert.equal(formatRate(12_500_000, "bytes"), "12 MiB/s");
  assert.equal(formatRate(1_048_576, "bytes"), "1.0 MiB/s");
  assert.equal(formatRate(1_048_576, "bits"), "8.4 Mbps");
});

test("unavailable and invalid rates cannot display a negative or nonfinite speed", () => {
  for (const value of [0, -1, NaN, Infinity]) {
    assert.equal(formatRate(value, "bits"), "0 Mbps");
    assert.equal(formatRate(value, "bytes"), "0 B/s");
  }
});
