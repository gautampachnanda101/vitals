# vitals — user guide

`vitals` is a single static binary that tells you **which resource is
constraining your machine right now and the exact command to fix it**. It
complements htop/btop, ncdu, nvtop and glances rather than replacing them: it
reads the same data source (gopsutil) and adds correlation, remediation and
local/cloud LLM diagnostics those tools do not provide.

Runs identically on macOS, Linux and Windows. No runtime dependencies.

## Quick start

```
vitals doctor        # the one-shot verdict: what's wrong + how to fix it
vitals top           # Activity-Monitor-style snapshot of everything
vitals memcheck      # RAM / swap / pressure with a health verdict
vitals memhogs       # which apps are eating memory + a stop command each
vitals clean --dry-run   # what a cleanup would remove
vitals llm           # every local and cloud LLM endpoint
```

Every command accepts `--no-color` (or set `NO_COLOR`). `top`, `llm` and
`doctor` accept `--json` for scripting.

## vitals doctor

Samples CPU, memory, swap, disk, thermal and any local LLM runtime, then
correlates them into a ranked list of findings, each with a concrete fix.
It catches the cases a plain gauge hides:

- CPU shows 90% busy but most of it is **I/O wait** — the bottleneck is the
  disk, not the CPU.
- RAM is at 95% but nothing is paging — most of it is **reclaimable cache**,
  not real pressure.
- Swap is filling and **paging out** — the machine is thrashing.
- The CPU or GPU is **thermally throttling** — sustained performance is capped.
- A model is running **on the CPU** because the runtime never got the GPU.
- A disk is **about to fill** — with a projected time-to-full.

Exit code: `0` healthy, `1` warning, `2` critical. Use it in CI or cron:

```
vitals doctor || echo "runner is unhealthy"
vitals doctor --json | jq -r '.findings[] | "\(.severity)\t\(.title)"'
```

## vitals llm

One command for every LLM endpoint, local or remote, over open APIs only.

**Local runtimes** — Ollama, LM Studio, llama.cpp, vLLM — are probed on their
default ports. For Ollama, each loaded model's VRAM footprint and the exact
**percentage offloaded to the GPU** is shown (`size_vram / size`). Below 100%
means layers are split between GPU and CPU and generation will be slow; drop to
a smaller quant or fewer layers.

**Cloud providers** — OpenAI, Anthropic, Groq, Mistral, Together, OpenRouter,
DeepSeek, xAI, Fireworks, Gemini, Ollama Cloud — are probed **only when the
matching API-key environment variable is set** (`OPENAI_API_KEY`,
`ANTHROPIC_API_KEY`, …). Nothing is contacted otherwise, and keys are never
printed or written to `--json`. Reachability and latency are reported.

Corporate / self-hosted gateways: point `--ollama-url` at your host, or set the
standard env var your gateway expects.

## vitals clean

Removes developer and OS caches, logs, temp directories and trash using the Go
standard library, and runs `brew` / `docker` / `npm` / `pip` prune when those
tools are on PATH. `--dry-run` measures without deleting; `--yes` skips the
prompt. The summary reports only the bytes it actually measured — never a
filesystem-delta guess.

## Shell completion

```
vitals completion bash | sudo tee /etc/bash_completion.d/vitals
vitals completion zsh  > "${fpath[1]}/_vitals"
vitals completion fish > ~/.config/fish/completions/vitals.fish
```

## Contextual help

```
vitals help              # command list
vitals help doctor       # full help for one command
vitals doctor -h         # the same, from the command itself
```
