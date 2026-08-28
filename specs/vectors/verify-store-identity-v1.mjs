#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";

const vectorPath = new URL("./store-identity-v1.json", import.meta.url);
const vector = JSON.parse(await readFile(vectorPath, "utf8"));

function frame(value) {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(value.length);
  return Buffer.concat([length, value]);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

const document = Buffer.concat([
  Buffer.from(vector.document_domain_hex, "hex"),
  frame(Buffer.from(vector.document_version, "utf8")),
  frame(Buffer.from(vector.identity_scheme, "utf8")),
  frame(Buffer.from(vector.nonce_hex, "hex")),
]);
const documentHex = document.toString("hex");
const documentDigest = `sha256:${sha256(document)}`;
const storeID = `store:v1:sha256:${sha256(Buffer.concat([
  Buffer.from(vector.store_id_domain_hex, "hex"),
  document,
]))}`;

const mismatches = [];
for (const [field, actual, expected] of [
  ["canonical_document_hex", documentHex, vector.canonical_document_hex],
  ["document_digest", documentDigest, vector.document_digest],
  ["store_id", storeID, vector.store_id],
]) {
  if (actual !== expected) mismatches.push(`${field}: computed ${actual}, expected ${expected}`);
}
if (mismatches.length > 0) {
  throw new Error(`store identity vector mismatch\n${mismatches.join("\n")}`);
}
console.log(`verified ${vector.vector_version} ${vector.store_id}`);
