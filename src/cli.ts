import { promises as fs } from "node:fs";
import path from "node:path";

import { ContractError, parseContract } from "./contract.js";
import { createReport, formatText, THESEUS_VERSION } from "./report.js";
import { verifyTarget } from "./runner.js";

const help = `Theseus ${THESEUS_VERSION}

Distribution parity tests for packaged CLIs.

Usage:
  theseus verify <package|directory|tgz> --contract <file> [--format text|json]
  theseus --help
  theseus --version

Exit codes:
  0  all declared contracts hold
  1  behavior drift
  2  contract, pack, install, or probe incomplete
`;

export async function main(argv = process.argv.slice(2)): Promise<number> {
  if (argv.length === 0 || argv.includes("--help") || argv[0] === "help") {
    process.stdout.write(help);
    return 0;
  }
  if (argv.length === 1 && argv[0] === "--version") {
    process.stdout.write(`${THESEUS_VERSION}\n`);
    return 0;
  }
  if (argv[0] !== "verify") {
    process.stderr.write(`theseus: unknown command ${argv[0]}\n`);
    return 2;
  }

  let parsed: { target: string; contractPath: string; format: "text" | "json" };
  try {
    parsed = parseVerifyArgs(argv.slice(1));
  } catch (error) {
    process.stderr.write(`theseus: ${message(error)}\n`);
    return 2;
  }

  try {
    const contractContent = await fs.readFile(path.resolve(parsed.contractPath), "utf8");
    const contract = parseContract(JSON.parse(contractContent) as unknown);
    const result = await verifyTarget(parsed.target, contract);
    const report = createReport(parsed.target, result);
    process.stdout.write(parsed.format === "json" ? `${JSON.stringify(report, null, 2)}\n` : formatText(report));
    return report.exitCode;
  } catch (error) {
    const detail = error instanceof SyntaxError ? `contract JSON is invalid: ${error.message}` : message(error);
    if (parsed.format === "json") {
      process.stdout.write(`${JSON.stringify({ schemaVersion: 1, tool: { name: "theseus", version: THESEUS_VERSION }, target: parsed.target, status: "incomplete", exitCode: 2, error: { code: "THS-INCOMPLETE-001", message: detail } }, null, 2)}\n`);
    } else {
      const prefix = error instanceof ContractError ? "contract" : "incomplete";
      process.stderr.write(`theseus ${prefix}: ${detail}\n`);
    }
    return 2;
  }
}

function parseVerifyArgs(argv: string[]): { target: string; contractPath: string; format: "text" | "json" } {
  if (argv.length === 0 || argv[0]?.startsWith("-")) throw new Error("verify requires a package, directory, or .tgz target");
  const target = argv[0]!;
  let contractPath = "theseus.config.json";
  let format: "text" | "json" = "text";
  for (let index = 1; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--contract") {
      const value = argv[index + 1];
      if (!value) throw new Error("--contract requires a file path");
      contractPath = value;
      index += 1;
    } else if (arg === "--format") {
      const value = argv[index + 1];
      if (value !== "text" && value !== "json") throw new Error("--format must be text or json");
      format = value;
      index += 1;
    } else {
      throw new Error(`unknown option ${String(arg)}`);
    }
  }
  return { target, contractPath, format };
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
