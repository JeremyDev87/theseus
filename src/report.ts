import type { VerificationReport, VerificationResult } from "./types.js";

export const THESEUS_VERSION = "0.1.0";

export function createReport(target: string, result: VerificationResult): VerificationReport {
  const status = result.incomplete.length > 0 ? "incomplete" : result.comparison.findings.length > 0 ? "drift" : "pass";
  const exitCode = status === "incomplete" ? 2 : status === "drift" ? 1 : 0;
  return {
    schemaVersion: 1,
    tool: { name: "theseus", version: THESEUS_VERSION },
    target,
    status,
    exitCode,
    ...result,
  };
}

export function formatText(report: VerificationReport): string {
  const lines = [
    `Theseus ${report.tool.version}`,
    `artifact  ${report.artifact.packageName}@${report.artifact.packageVersion}`,
    `sha256    ${report.artifact.sha256}`,
    `profiles  ${report.receipts.map((receipt) => receipt.profile).join(", ") || "none"}`,
    `status    ${report.status.toUpperCase()} (exit ${report.exitCode})`,
  ];
  for (const incomplete of report.incomplete) {
    lines.push(`! ${incomplete.code} ${incomplete.profile}/${incomplete.stage}${incomplete.probe ? `/${incomplete.probe}` : ""}: ${incomplete.message}`);
  }
  for (const finding of report.comparison.findings) {
    lines.push(`! ${finding.code} ${finding.message}`);
    if (finding.digests) {
      for (const [profile, digest] of Object.entries(finding.digests)) lines.push(`  ${profile}: ${digest}`);
    }
  }
  for (const allowed of report.comparison.allowedDifferences) {
    lines.push(`~ ALLOWED ${allowed.probe}/${allowed.field} ${allowed.profiles.join("↔")}: ${allowed.reason}`);
  }
  return `${lines.join("\n")}\n`;
}
