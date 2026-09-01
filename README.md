# vitals

A single, cross-platform helper binary that replaces the pile of shell /
PowerShell scripts this repo used to carry. One static Go binary, no external
script dependencies, identical behaviour on **macOS, Linux and Windows**.

The philosophy is *complement, don't replace*: where an established open-source
tool already does the job well (`htop`/`btop`, `ncdu`, `nvtop`, `glances`,
`docker`, `brew`), `vitals` leans on the same underlying data source
([`gopsutil`](https://github.com/shirou/gopsutil)) and adds a consolidated,
scriptable view plus diagnostics those tools don't provide — most notably
per-model GPU-offload for local LLM runtimes.

## Build

```sh
go build -o vitals .          # current platform
./build.sh 1.0.0               # all platforms -> ./dist/
```

## Commands

| Command    | What it does |
|------------|--------------|
| `top`      | Activity-Monitor-style snapshot — system CPU / RAM / load, disk & network I/O, and the top processes by CPU or memory. `--watch` turns it into a live dashboard. |
| `clean`    | Cross-platform disk cleanup: developer caches (`~/.cache`, npm, yarn, pip, gradle…), OS caches/logs/trash, temp dirs, plus optional `brew` / `docker` / `apt` prune when those tools are present. `--dry-run` first. |
| `memhogs`  | Ranks application *families* (Chrome, VS Code, Docker, Ollama, JetBrains…) and individual processes by memory, with a concrete kill/prune action for each. |
| `memcheck` | Advanced RAM / swap / pressure overview with a health verdict. |
| `llm`      | Deep diagnostics for local LLM runtimes: host CPU/RAM of the server process, which provider endpoints are up (Ollama, LM Studio, llama.cpp, vLLM), and Ollama's per-model VRAM footprint with the exact **percentage offloaded to GPU**. |
| `version`  | Print version. |

### Examples

```sh
vitals top --sort mem --watch --interval 1s
vitals clean --dry-run
vitals memhogs --top 20
vitals memcheck
vitals llm
vitals llm --json | jq '.models[] | {name, gpu_offload_percent}'
```

`--json` (on `top` and `llm`) emits a machine-readable snapshot for scripting or
feeding a dashboard.

## Why the `llm` insight matters

Standard task managers show your CPU pinned at 100% and imply it's doing useful
work. When Ollama can't fit a model entirely in VRAM it splits layers between GPU
and CPU, and the CPU spends most of its cycles waiting on GPU serialization.
`vitals llm` reads Ollama's `/api/ps` allocation engine and reports
`size_vram / size` as a percentage:

- **≥ 100%** – fully on GPU, optimal throughput.
- **0–99%** – partial offload; context shifting will spike CPU and slow
  generation. Drop to a smaller quant (e.g. `Q8_0` → `Q4_K_M`) or fewer layers.
- **0%** – CPU-only fallback, bottlenecked on system RAM bandwidth.

## Layout

```
main.go              CLI dispatcher
internal/ui          terminal formatting helpers
internal/monitor     `top`
internal/clean       `clean`
internal/memhogs     `memhogs`
internal/memcheck    `memcheck`
internal/llm         `llm`
legacy/              the original shell scripts, kept for reference
```
