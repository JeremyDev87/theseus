#!/usr/bin/env node
if (process.argv.includes("--cwd")) {
  process.stdout.write(`${process.cwd()}\n`);
  process.exit(0);
}
if (process.argv.includes("--hang")) {
  setInterval(() => {}, 1_000);
} else if (process.argv.includes("--help")) {
  process.stdout.write("parity-fixture usage\n");
  process.exit(0);
} else {
  process.stderr.write("unknown command\n");
  process.exit(2);
}
