import assert from "node:assert/strict";
import test from "node:test";

import { stableStringify } from "../src/hash.js";

test("stableStringify sorts object keys by code point without locale rules", () => {
  assert.equal(stableStringify({ ä: 3, a: 2, Z: 1 }), '{"Z":1,"a":2,"ä":3}');
  assert.equal(stableStringify({ outer: { ä: 3, a: 2, Z: 1 } }), '{"outer":{"Z":1,"a":2,"ä":3}}');
});
