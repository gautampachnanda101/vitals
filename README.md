# vitals

**[gautampachnanda101.github.io/vitals](https://gautampachnanda101.github.io/vitals/)**

A single static binary that doesn't just draw gauges — it **names the one
resource constraining your machine right now and the exact command to fix it**.
macOS, Linux and Windows; no runtime dependencies.

`vitals` complements htop/btop, ncdu, nvtop and glances rather than replacing
them: it reads the same data source ([gopsutil](https://github.com/shirou/gopsutil))
and adds the layer above — cross-domain correlation, concrete remediation, and
local **and cloud** LLM diagnostics those tools don't provide.

```console
$ vitals doctor
● CRITICAL   Memory is your bottleneck — not CPU
             swap 91% full, paging out at 40 MB/s   → the machine is stalling on disk
  → free RAM now: `vitals memhogs` then quit the top consumers
  → macOS: `sudo purge`
  exit 2
```

## Install

### macOS / Linux

```sh
brew install gautampachnanda101/tap/vitals
# or, without Homebrew:
curl -fsSL https://raw.githubusercontent.com/gautampachnanda101/vitals/main/install.sh | sh
```

### Windows

```powershell
scoop bucket add gautampachnanda101 https://github.com/gautampachnanda101/scoop-bucket
scoop install vitals
```

**Direct download** — grab a tarball from the
[latest release](https://github.com/gautampachnanda101/vitals/releases/latest):
`vitals_$(uname -s)_$(uname -m).tar.gz`.

**From source** — `go build -o vitals .` (Go 1.27+), or `./build.sh 1.0.0` for
every platform into `./dist/`.

## Commands

| Command | What it does |
|---|---|
| `doctor` | Correlates CPU / memory / swap / disk / thermal / GPU / LLM into a ranked verdict — what's wrong and the fix. Exit code **0** healthy, **1** warning, **2** critical. |
| `dashboard` | Serves the same correlation as browsable pages — one per resource, only the ones this machine actually has. Loopback-only (`127.0.0.1`), opens your browser automatically. |
| `cpu` `mem` `disk` `net` `power` | Deep dive on one resource: the current numbers plus just that resource's findings. |
| `gpu` | Per-GPU VRAM / util / temp / power / clocks and the processes holding VRAM, via `nvidia-smi` / `rocm-smi` / `ioreg`. |
| `top` | Activity-Monitor-style snapshot: system CPU / RAM / load, **per-second** disk & network I/O, top processes. `--watch` for a live redraw. |
| `memhogs` | Ranks application families (resolved from the OS's own app identity — `.app` bundle, cgroup scope, install dir) and individual processes by memory, with an OS-correct stop command each. |
| `memcheck` | RAM / swap / pressure breakdown with a ranked verdict. |
| `llm` | Every local (Ollama, LM Studio, llama.cpp, vLLM) and cloud (OpenAI, Anthropic, Groq, Mistral, …, Ollama Cloud) LLM endpoint, over open OpenAI-compatible APIs — probed only when the provider's API-key env var is set. Per-model GPU offload %. Keys are never printed. |
| `llm fit <model>` | The largest quant of a model that fully fits your VRAM, with the spill % for the ones that don't. |
| `clean` | Cross-platform disk cleanup (dev/OS caches, logs, temp, trash) + `brew`/`docker`/`npm`/`pip` prune when present. `--dry-run` first. |
| `serve` / `export` | Prometheus `/metrics` (OpenTelemetry semantic-convention names) — as a server or a one-shot dump. |
| `mcp` | Run as a Model Context Protocol server on stdio (`claude mcp add vitals -- vitals mcp`). A Claude Code plugin (skill + `/vitals-*` commands + optional pre-flight hook) is in [`plugin/`](plugin/). |
| `guide` / `help [<cmd>]` / `completion <shell>` | Embedded user guide, contextual help, and bash/zsh/fish completion. |

`--json` on `doctor`, the resource commands, `top` and `llm`. `--no-color`
everywhere (also honours `NO_COLOR`).

### Examples

```sh
vitals doctor --json | jq -r '.findings[] | "\(.severity)\t\(.title)"'
vitals doctor || echo "machine unhealthy"          # use the exit code in CI
vitals top --sort mem --watch
vitals llm fit qwen2.5:32b
OPENAI_API_KEY=sk-... vitals llm
vitals serve                                       # loopback only: 127.0.0.1:9100
vitals serve --addr :9600                          # bind every interface — Grafana/Prometheus on another host
```

## Why the LLM insight matters

Standard task managers show your CPU pinned at 100% and imply it's doing useful
work. When a model doesn't fit in VRAM the runtime splits layers between GPU and
CPU, and the CPU spends most of its cycles waiting on GPU serialization. `vitals`
reads Ollama's `/api/ps` and reports `size_vram / size` as a percentage —
**≥ 100 %** fully on GPU, **0–99 %** partial offload (slow, context-shifting),
**0 %** CPU-only fallback.

## Design

Every module answers three things, not one: the gauge, what it means *causally*
(compute-bound vs I/O-wait vs contention vs thermal vs reclaimable), and the
exact fix. The correlation engine (`internal/doctor`) is a pure function over a
`Snapshot`, so it's tested entirely from fixtures. Full write-up:
[docs/architecture/design.md](docs/architecture/design.md).

```text
main.go            CLI dispatcher
internal/doctor    the correlation engine + `doctor` / resource commands
internal/diag      severity / finding / report vocabulary
internal/gpu       nvidia-smi / rocm-smi / ioreg telemetry
internal/llm       local + cloud provider probing, per-model offload, `llm fit`
internal/metrics   Prometheus exporter (OTel semconv names)
internal/mcp       Model Context Protocol server
internal/monitor   `top`
internal/memhogs   `memhogs`  (OS-native app-family resolution + families.json)
internal/memcheck  `memcheck`
internal/clean     `clean`
internal/ui        terminal formatting helpers
internal/help      command docs, contextual help, shell completion
```

## Documentation

- [docs/](docs/index.md) — the full docs site (also builds with `mkdocs`, see `mkdocs.yml`)
  - [User guide](docs/user-guide.md) — every command, with runnable examples (same content as `vitals guide`)
  - [Architecture](docs/architecture/design.md) — design + the review that shaped it
  - [Roadmap](docs/roadmap/index.md) — shipped, in progress, and planned
- [AGENTS.md](AGENTS.md) — build/test/lint commands and repo conventions
- [CONTRIBUTING.md](CONTRIBUTING.md) · [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) · [SECURITY.md](SECURITY.md)

## License

MIT © 2026 Gautam Pachnanda
