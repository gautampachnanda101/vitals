---
name: system-diagnostics
description: >-
  Diagnose why the machine is slow, unresponsive, out of memory, or why a build,
  test run, or local LLM is crawling. Use when the user says things like "my
  machine is slow", "everything froze", "out of memory / OOM killed", "disk is
  full", "the fans are going", "ollama is really slow", "the GPU isn't being
  used", or asks what is hogging CPU / RAM / disk. Runs the `vitals` binary to
  get a correlated verdict and a concrete fix.
---

# System diagnostics with vitals

`vitals` is a single static binary that correlates CPU, memory, swap, disk,
thermal, GPU and local-LLM signals into a ranked verdict — what is actually
constraining the machine right now and the exact command to fix it.

## First: is `vitals` installed?

```sh
command -v vitals
```

If it is missing, tell the user how to install it and stop:

```sh
brew install gautampachnanda101/tap/vitals
# or
curl -fsSL https://raw.githubusercontent.com/gautampachnanda101/vitals/main/install.sh | sh
```

## Then: get the verdict

```sh
vitals doctor --json
```

The payload (schema `1.0.0`) has:

- `verdict` — `ok` | `warning` | `critical`
- `exit_code` — `0` | `1` | `2` (the process also exits with this)
- `findings[]` — `{ severity, title, detail, fixes[] }`, most severe first
- `snapshot` — the raw readings (`cpu`, `memory`, `disks`, `gpus`, `llm`, `net`,
  `power`, `thermal`)

## How to respond

1. Lead with the **single most severe finding** — its `title` and `detail`.
2. Give the **first entry of its `fixes`** as the recommended action; list the
   rest as alternatives.
3. If the user wants more on one area, drill in with the matching command and
   relay its findings:
   - `vitals mem` / `vitals cpu` / `vitals disk` / `vitals net` / `vitals power`
   - `vitals memhogs` — which apps are eating memory, with a stop command each
   - `vitals gpu` — VRAM / util / temp, and processes holding VRAM
   - `vitals llm` — local + cloud LLM endpoints and per-model GPU offload %
   - `vitals llm fit <model>` — the largest quant that fits this machine's VRAM
4. **Do not run any suggested `kill` / `pkill` / `docker` / `purge` / `clean`
   command yourself.** Show it to the user and let them decide. `vitals clean`
   in particular deletes files — only ever suggest `vitals clean --dry-run`
   first.
5. If `verdict` is `ok`, say so plainly and stop — don't invent problems.

## Notes

- `vitals doctor` needs no external tools; GPU detail additionally uses
  `nvidia-smi` / `rocm-smi` / `ioreg` if present.
- Cloud LLM providers are only contacted when their API-key env var is set, and
  keys are never printed.
