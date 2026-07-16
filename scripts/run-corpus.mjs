#!/usr/bin/env node
import { spawnSync } from "node:child_process";

const cases = [
  { name: "maximus-strict", target: "@jeremyfellaz/maximus@0.1.4", contract: "test/corpus/maximus.json", expected: 1 },
  { name: "maximus-allowed", target: "@jeremyfellaz/maximus@0.1.4", contract: "test/corpus/maximus-allowed.json", expected: 0 },
  { name: "kratos", target: "@jeremyfellaz/kratos@0.3.7", contract: "test/corpus/kratos.json", expected: 1 },
  { name: "legolas", target: "@jeremyfellaz/legolas@0.1.7", contract: "test/corpus/legolas.json", expected: 0 },
  { name: "ast-grep", target: "@ast-grep/cli@0.44.1", contract: "test/corpus/ast-grep.json", expected: 2 },
];

let failed = false;
for (const item of cases) {
  const result = spawnSync(
    process.execPath,
    ["bin/theseus.js", "verify", item.target, "--contract", item.contract, "--format", "json"],
    { cwd: process.cwd(), encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
  );
  let report;
  try {
    report = JSON.parse(result.stdout);
  } catch {
    console.error(`FAIL ${item.name}: output was not JSON\n${result.stdout}\n${result.stderr}`);
    failed = true;
    continue;
  }
  const actual = result.status ?? 2;
  const findingCodes = report.comparison?.findings?.map((finding) => finding.code).join(",") || "none";
  console.log(`${actual === item.expected ? "PASS" : "FAIL"} ${item.name}: exit=${actual} status=${report.status} findings=${findingCodes}`);
  if (actual !== item.expected || report.exitCode !== item.expected) failed = true;
}

process.exitCode = failed ? 1 : 0;
