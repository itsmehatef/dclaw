#!/usr/bin/env node
// dclaw-agent entrypoint — spawns pi-mono's print-mode CLI with the user's prompt
// and inherits its stdio. Exits with pi's exit code.

import { spawn } from "node:child_process";
import * as os from "node:os";

const prompt = process.argv.slice(2).join(" ").trim();

if (!prompt) {
  console.error("usage: dclaw-agent <prompt>");
  console.error("");
  console.error("env:");
  console.error("  ANTHROPIC_API_KEY   Anthropic API key (required)");
  console.error("  ANTHROPIC_OAUTH_TOKEN  OAuth token (takes precedence if set)");
  console.error("");
  console.error("example:");
  console.error('  docker run --rm -e ANTHROPIC_API_KEY=sk-... -v $(pwd):/workspace dclaw-agent:v0.1 "list files"');
  process.exit(2);
}

if (!process.env.ANTHROPIC_API_KEY && !process.env.ANTHROPIC_OAUTH_TOKEN) {
  console.error("error: ANTHROPIC_API_KEY (or ANTHROPIC_OAUTH_TOKEN) not set");
  process.exit(2);
}

const child = spawn(
  "pi",
  ["-p", "--no-session", prompt],
  {
    stdio: ["ignore", "inherit", "inherit"],
    env: process.env,
  },
);

child.on("error", (err) => {
  console.error("error: failed to spawn pi:", err.message);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    const signum = os.constants.signals[signal] ?? 0;
    console.error(`pi terminated by signal ${signal} (exit ${128 + signum})`);
    process.exit(128 + signum);
  }
  process.exit(code ?? 1);
});
