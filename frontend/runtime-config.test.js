import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { missingConfigKeys, requiredConfigKeys, usesDevAuth } from "./runtime-config.js";

test("cloud config requires the cognito fields and rejects placeholders", () => {
  const example = JSON.parse(readFileSync(new URL("./config.example.json", import.meta.url), "utf8"));
  assert.equal(usesDevAuth(example), false);
  assert.deepEqual(requiredConfigKeys(example), ["apiBaseUrl", "cognitoDomain", "clientId"]);
  assert.deepEqual(missingConfigKeys(example), ["cognitoDomain", "clientId"]);
});

test("local kind config is same-origin dev auth", () => {
  const local = JSON.parse(readFileSync(new URL("../deploy/overlays/local/app/config.local.json", import.meta.url), "utf8"));
  assert.equal(usesDevAuth(local), true);
  assert.equal(local.apiBaseUrl, "/api/v1");
  assert.deepEqual(missingConfigKeys(local), []);
});
