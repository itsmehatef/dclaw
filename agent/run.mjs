#!/usr/bin/env node
// dclaw-agent entrypoint. By default it spawns pi-mono's print-mode CLI with
// the user's prompt and inherits its stdio. A lightweight DeepSeek chat path is
// available for smoke/one-shot chat where pi-mono has no native DeepSeek
// provider.

if (process.getuid() === 0) {
  console.error("error: dclaw-agent must not run as uid 0; daemon should have applied --user=1000:1000");
  process.exit(70);
}

import { spawn } from "node:child_process";
import * as os from "node:os";

const prompt = process.argv.slice(2).join(" ").trim();
const PI_PROVIDER_ENV_KEYS = [
  "ANTHROPIC_API_KEY",
  "ANTHROPIC_OAUTH_TOKEN",
  "OPENAI_API_KEY",
  "OPENROUTER_API_KEY",
  "GEMINI_API_KEY",
  "GOOGLE_API_KEY",
  "ZAI_API_KEY",
  "MISTRAL_API_KEY",
  "GROQ_API_KEY",
];

if (!prompt) {
  console.error("usage: dclaw-agent <prompt>");
  console.error("");
  console.error("env:");
  console.error("  ANTHROPIC_API_KEY       Anthropic API key for pi-mono");
  console.error("  ANTHROPIC_OAUTH_TOKEN   Anthropic OAuth token for pi-mono");
  console.error("  DEEPSEEK_API_KEY        DeepSeek API key for simple chat");
  console.error("  DEEPSEEK_MODEL          DeepSeek model (default: deepseek-v4-flash)");
  console.error("  DCLAW_AGENT_PROVIDER    Set to deepseek to force the DeepSeek path");
  console.error("");
  console.error("example:");
  console.error('  docker run --rm -e ANTHROPIC_API_KEY=sk-... -v "$(pwd):/workspace" dclaw-agent:v0.1 node /app/run.mjs "list files"');
  process.exit(2);
}

const provider = (process.env.DCLAW_AGENT_PROVIDER ?? "").trim().toLowerCase();
const hasDeepSeekKey = Boolean(process.env.DEEPSEEK_API_KEY);
const hasPiProviderKey = PI_PROVIDER_ENV_KEYS.some((key) => Boolean(process.env[key]));

if (provider === "deepseek" || (!provider && hasDeepSeekKey && !hasPiProviderKey)) {
  await runDeepSeek(prompt);
  process.exit(0);
}

if (!hasPiProviderKey) {
  console.error("error: no supported provider key set (ANTHROPIC_API_KEY, ANTHROPIC_OAUTH_TOKEN, DEEPSEEK_API_KEY, or a pi-mono provider key)");
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

async function runDeepSeek(userPrompt) {
  const apiKey = process.env.DEEPSEEK_API_KEY;
  if (!apiKey) {
    console.error("error: DEEPSEEK_API_KEY not set");
    process.exit(2);
  }

  const model = process.env.DEEPSEEK_MODEL || "deepseek-v4-flash";
  const baseURL = (process.env.DEEPSEEK_BASE_URL || "https://api.deepseek.com").replace(/\/+$/, "");
  let response;
  let body;
  try {
    response = await fetch(`${baseURL}/chat/completions`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model,
        messages: [{ role: "user", content: userPrompt }],
        stream: false,
        temperature: 0,
      }),
    });
    body = await response.text();
  } catch (err) {
    console.error(`deepseek request failed: ${err.message}`);
    process.exit(1);
  }

  if (!response.ok) {
    console.error(`deepseek request failed (${response.status}): ${body}`);
    process.exit(1);
  }

  let data;
  try {
    data = JSON.parse(body);
  } catch (err) {
    console.error(`deepseek returned invalid JSON: ${err.message}`);
    process.exit(1);
  }

  const text = data?.choices?.[0]?.message?.content;
  if (typeof text !== "string" || text.length === 0) {
    console.error("deepseek response did not include choices[0].message.content");
    process.exit(1);
  }

  process.stdout.write(text);
  if (!text.endsWith("\n")) {
    process.stdout.write("\n");
  }
}
