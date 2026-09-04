# vitals — user guide

[docs](index.md) / **User guide**

**[gautampachnanda101.github.io/vitals](https://gautampachnanda101.github.io/vitals/)**

`vitals` tells you which resource is slowing your machine down and what to
do about it. It complements htop/btop, ncdu/gdu/dust, nvtop and glances
instead of replacing them: same underlying data (gopsutil), plus
correlation across resources, remediation, trend detection, and local/cloud
LLM diagnostics. `vitals tools`, `explore` and `live` can detect, install,
and hand off to those other tools directly when their specialty view is
what you actually want.

One static binary, no bundled installer, no phone-home. gopsutil is the
one dependency for system data; a small terminal-reliability group
(cross-platform color and real terminal-width detection) is the one
deliberate exception to "hand-write it instead." Runs the same way on
macOS, Linux and Windows.

**On this page** — `vitals guide --web` builds its own navigable table
of contents from the headings below automatically; this plain list is
for reading the raw file itself (GitHub, an editor, `vitals guide --raw`,
the plain `vitals guide` terminal view) without one:

- [Quick start](#quick-start)
- [Quick reference](#quick-reference)
- [vitals doctor](#vitals-doctor)
- [vitals dashboard](#vitals-dashboard)
- [Resource deep dives: cpu, mem, disk, net, power](#resource-deep-dives-cpu-mem-disk-net-power)
- [vitals advice](#vitals-advice)
- [vitals llm](#vitals-llm)
- [vitals top, memhogs, memcheck](#vitals-top-memhogs-memcheck)
- [vitals clean, dupes, tools, explore, live](#vitals-clean-dupes-tools-explore-live)
- [vitals gpu](#vitals-gpu)
- [Automation and integration](#automation-and-integration)
- [Configuration file](#configuration-file)
- [Shell completion](#shell-completion)
- [Contextual help](#contextual-help)

## Quick start

```bash
vitals doctor            # the one-shot verdict: what's wrong and how to fix it
vitals advice            # ask a local or cloud LLM to interpret that report
vitals top               # Activity-Monitor-style snapshot of everything
vitals cpu|mem|disk|net|power   # a deep dive into one resource
vitals memhogs           # which apps are eating memory, and how to stop each
vitals clean --dry-run   # what a cleanup would remove
vitals dupes             # find duplicate files (reports only, never deletes)
vitals llm               # every local and cloud LLM endpoint
vitals tools             # which companion tools are installed
```

Every command takes `--no-color` (or honors `NO_COLOR`). `doctor` and each
resource deep dive also take `--json`, `--output FILE`, `--ci`, and
`--quiet`/`-q` for scripting.

## Quick reference

Everything on one page — command groups, universal flags, exit codes, and
where config lives.

- **Diagnose** — `vitals doctor` · `vitals cpu`/`mem`/`disk`/`net`/`power` · `vitals doctor --compare a.json b.json`
- **Browse** — `vitals dashboard` — the same correlation as browsable pages, loopback-only
- **Understand (LLM)** — `vitals advice` · `vitals llm` · `vitals llm fit <model>` · `vitals gpu`
- **Fix & reclaim** — `vitals clean --dry-run` · `vitals dupes --hardlink` · `vitals tools --install btop` · `vitals explore` · `vitals live`
- **Watch live** — `vitals top --watch --sort mem` · `vitals memhogs --watch` · `vitals memcheck`
- **Automate** — `vitals doctor --json --output r.json` · `vitals doctor --webhook <url>` · `vitals serve` / `export` · `vitals mcp`
- **Configure** — `<config dir>/vitals/config.toml` · `<config dir>/vitals/families.json`

Flags that work the same way on every command:

- `--json` — machine-readable output
- `--output FILE` — save alongside any other mode
- `--ci` — one grep-friendly line
- `-q`, `--quiet` — exit code only, nothing printed
- `-v`, `--verbose` — everything the default view has no room for
- `--no-color` — or set `NO_COLOR` in the environment

Exit codes: `0` healthy — nothing to do, `1` warning — worth a look, `2`
critical — act now.

Config file locations (see [Configuration file](#configuration-file)):

```bash
# macOS:   ~/Library/Application Support/vitals/
# Linux:   ~/.config/vitals/  (or $XDG_CONFIG_HOME/vitals/)
# Windows: %AppData%\vitals\
```

Get help without leaving the terminal: `vitals help <command>`,
`vitals guide` (or `guide --web` to render this file in a browser), and
`vitals completion bash|zsh|fish`.

## vitals doctor

`doctor` samples CPU, memory, swap, disk, thermal, network, power, GPU, and
any local LLM runtime, then correlates them into a ranked list of findings,
each with a concrete fix. Every run prints an at-a-glance line —
`cpu 4%  mem 74%  disk 59% (/)  battery 100%` — whether the verdict is
healthy or not, so "healthy" is something you can check yourself instead of
taking on faith.

This is what the correlation catches that a plain gauge would miss:

- CPU reads 90% busy, but most of it is I/O wait. The disk is the
  bottleneck, not the CPU.
- RAM sits at 95%, but the kernel still reports plenty available. Most of
  that is reclaimable cache, not real pressure.
- Swap is filling and actively paging out. The machine is thrashing.
- One core is pinned near 100% while the rest idle. That's a single-thread
  bottleneck — adding cores won't help.
- A process's memory has climbed steadily across every recent sample with
  no plateau. Likely a leak, and `doctor` names the PID.
- A disk is projected to fill, with a real growth rate tracked across runs,
  not a guess from a single snapshot.
- A model is running entirely on the CPU because the runtime never got the
  GPU. When that happens, `doctor` also checks whether the GPU driver
  itself is even reachable, so you know whether to blame the driver or the
  runtime.

Wherever a finding traces back to one process, `doctor` names it — PID,
name, and the relevant percentage — instead of just saying "CPU is high."

Exit codes are `0` healthy, `1` warning, `2` critical, which makes `doctor`
usable directly in CI or cron:

```bash
vitals doctor || echo "runner is unhealthy"
vitals doctor --ci                                    # one grep-friendly line for logs
vitals doctor -q; echo $?                             # exit code only, nothing printed
vitals doctor --json | jq -r '.findings[] | "\(.severity)\t\(.title)"'
vitals doctor --output report.json                    # save a snapshot, on top of any other output mode
vitals doctor --webhook https://hooks.slack.com/...   # notify, but only when something needs attention
vitals doctor --compare before.json after.json        # diff two saved reports
```

The `--json` payload carries a `schema_version` (currently `1.1.0`), and the
shape only ever grows — nothing is renamed or removed without a major
version bump. `vitals doctor --schema` prints the full JSON Schema. The
same envelope comes back from the MCP `system_health` tool and from
`vitals cpu|mem|disk|net|power --json`, which adds one extra `resource`
field.

### Trend detection

`doctor` keeps a small rolling history of its own readings (24 hours, or
2000 samples) in the OS config directory, purely to answer "has this been
getting worse" — the memory-leak finding above depends on it. It only
records on an actual `vitals doctor` run, never during a `vitals
serve`/`export` Prometheus scrape, and if the history file can't be written
for any reason, `doctor` just skips it rather than failing the command.

## vitals dashboard

`dashboard` serves the same correlation `doctor` prints in a terminal as
browsable pages instead — an overview, one page per resource, an advice
page, and a clean page. It binds `127.0.0.1` only and opens your default
browser automatically; nothing leaves this machine, the same promise
`vitals guide --web` makes.

```bash
vitals dashboard                # random port, opens your browser
vitals dashboard --addr :8080   # a stable port to bookmark — still 127.0.0.1 only
vitals dashboard --no-open      # print the URL instead of launching a browser
```

Each page only appears in the nav when this machine can actually offer it:
no GPU page without a GPU, no power page without a battery. The advice
page is always available — its rule-based findings need no LLM at all;
an LLM, when reachable, only adds AI commentary on top (see
[vitals advice](#vitals-advice)). Press Ctrl+C in the terminal that
launched it to stop.

The clean page mirrors `vitals clean` for the browser: a **Preview**
button measures what a real cleanup would reclaim, with no filesystem
mutation, and only once that's shown does an **Apply** button appear.
Apply asks for confirmation in the browser before it runs, then
performs the same cache/log/temp cleanup `vitals clean` does — this
genuinely deletes files, the same as running the CLI command without
`--dry-run`. Every mutating request the dashboard accepts is rejected
unless it comes from the dashboard's own page (a same-origin check,
independent of the in-page confirmation) — a browser tab from another
site can never trigger a cleanup on your machine.

## Resource deep dives: cpu, mem, disk, net, power

`vitals <resource>` shows the current numbers for one resource and only its
findings — quicker than the full `doctor` pass, and each one does more than
a bare gauge:

- **cpu** — the usage split (user+sys vs. I/O-wait vs. steal), load against
  core count, per-core spread to catch a single-thread bottleneck, clock
  and package temperature, and the actual process consuming the most CPU.
- **mem** — RAM used, the kernel's own available-without-swapping figure
  rather than a raw used percentage, swap-in/out rates rather than
  cumulative counters, and the actual process holding the most RSS.
- **disk** — per-mount usage, including network and remote mounts (NFS,
  SMB, AFP). A mount that's temporarily unreachable is skipped rather than
  hung on, since a dead network share can block indefinitely at the OS
  level. Also shown: device util/await/IOPS, inode headroom (space can
  look fine while new files still fail to create), a tracked growth rate
  with projected time-to-full, and a reclaimable-cache estimate — the same
  directories `vitals clean` would touch, actually measured and capped to
  a short time budget so a huge cache can't stall the command.
- **net** — per-interface rx/tx, the top remote peers by established TCP
  connection count, and a DNS-resolution latency check, which tells apart
  "DNS is slow" from "the link is slow" without needing raw-socket
  privileges.
- **power** — battery charge, the OS runtime estimate, health versus design
  capacity, charge direction (draining while plugged in is its own
  finding), and macOS Low Power Mode state, which changes performance
  expectations and is otherwise invisible.

All five take `--json`, `--output FILE`, `--ci`, and `--quiet`/`-q`, same as
`doctor`.

```bash
vitals disk                              # per-mount space, IOPS, reclaimable estimate
vitals cpu --json | jq '.snapshot.cpu.per_core_percent'
vitals net --ci                          # one line, for a log
vitals power -q; echo $?                 # exit code only
vitals mem --output mem-snapshot.json
```

## vitals advice

`advice` answers in two parts, and the first one needs no LLM at all:
`doctor`'s own rule-based findings and fixes print immediately, exactly
as `vitals doctor` would show them. Then, if a local or cloud LLM is
reachable, its synthesis prints underneath as **AI commentary** — shared
root causes across findings, what matters most when there's more than
one issue — a complement to the heuristic answer, never a replacement
for it. No LLM configured, or none reachable, means you still get a
useful answer; it just stops after the heuristic half.

It never assumes a runtime is installed. Provider selection tries, in
order, a local Ollama server, then LM Studio, then llama.cpp, then vLLM,
and only actually uses one once it answers with at least one model
available — not just because its default port is configured. If none of
them respond, it falls back to the first cloud provider with a configured
API key (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GROQ_API_KEY`, and so on),
using the same provider detection as `vitals llm`. `--provider` forces one
specific choice, local or cloud, instead of auto-detecting. Not sure which
local model you have pulled? `vitals llm` lists a reachable local
provider's model names directly — see [vitals llm](#vitals-llm) below —
so there's no need to go check with the provider's own CLI first.

```bash
vitals advice
vitals advice --provider ollama --model llama3.1:8b
OPENAI_API_KEY=sk-... vitals advice --provider openai
vitals advice --provider lmstudio --lmstudio-url http://localhost:1234
vitals advice --json | jq -r .heuristic_advice   # always present, no LLM required
vitals advice --json | jq -r .llm_advice         # present only when a provider answered
```

`--json`'s `source` field is `"heuristic"` or `"heuristic+llm"`, and
`llm_error` is set instead of `llm_advice` when no provider answered —
the payload always has something to show, it just tells you honestly
which half of it is present.

## vitals llm

One command for every LLM endpoint, local or remote, built entirely on open
APIs.

Local runtimes — Ollama, LM Studio, llama.cpp, vLLM — are probed on their
default ports. A reachable local provider's line lists every model it has
pulled, not just a count — copy a name straight from here into `vitals
advice --provider ollama --model <name>` or `vitals llm fit <name>`
instead of checking with `ollama list` first. Cloud providers still show
only a count: a hosted catalogue can run to dozens of entries not chosen
by name the same way. For Ollama, each loaded model's VRAM footprint and
its exact GPU-offload percentage are also shown (`size_vram / size`).
Below 100% means layers are split between GPU and CPU, and generation
will be slow; try a smaller quant or fewer layers. When a model is
running entirely on the CPU, `llm` also checks whether the GPU driver
itself (`nvidia-smi`/`rocm-smi`) is reachable, since that's the standard
first troubleshooting step — it tells you whether to blame the driver,
the VRAM budget, or the runtime.

Cloud providers — OpenAI, Anthropic, Groq, Mistral, Together, OpenRouter,
DeepSeek, xAI, Fireworks, Gemini, Ollama Cloud — are probed only when their
matching API-key environment variable is set. Nothing is contacted
otherwise, and keys are never printed or written to `--json`. What comes
back is just reachability and latency.

`vitals llm fit <model>` recommends the largest quantization of a named
model that actually fits the VRAM available on this machine.

For a corporate or self-hosted gateway, point `--ollama-url` at it, or set
whichever environment variable the gateway expects.

```bash
vitals llm
OPENAI_API_KEY=sk-... vitals llm
vitals llm --json | jq '.models[] | {name, gpu_offload_percent}'
vitals llm fit qwen2.5:32b    # largest quant that fits your VRAM
```

## vitals top, memhogs, memcheck

- **top** — an Activity-Monitor-style snapshot: system CPU/RAM/load,
  per-second disk and network I/O, and the top processes by CPU or memory
  (`--sort mem`). The RAM line also shows the OS-reported breakdown (wired,
  active, inactive on macOS; buffers, cached on Linux), because the
  process list below it won't sum to the total used, and this explains why.
  `--watch` turns it into a live dashboard.
- **memhogs** — groups processes into application families (Chrome, VS
  Code, Docker, JetBrains, Ollama, and so on), so twenty "Chrome Helper"
  rows collapse into one line. Ranks families and individual processes by
  resident memory and prints an OS-correct stop command for each.
  `--watch` for a live view. You can add your own family rules — see
  [Extending memhogs](#extending-memhogs) below.
- **memcheck** — a detailed physical-memory and swap breakdown (active,
  inactive, wired/cached, buffers, whichever your OS exposes), followed by
  a ranked verdict with concrete remedies.

```bash
vitals top --sort mem
vitals top --watch --interval 1s
vitals top --json | jq '.processes[0]'
vitals memhogs --top 30
vitals memhogs --watch
vitals memcheck
```

## vitals clean, dupes, tools, explore, live

The disk-cleanup family, from "reclaim space right now" to "let me look
around and decide myself":

- **clean** — removes developer and OS caches, logs, temp directories and
  trash using the Go standard library, and runs `brew`/`docker`/`npm`/`pip`
  prune, plus `flatpak`/`dism.exe` component-store cleanup on Linux and
  Windows, whenever those tools are already on PATH. `--dry-run` measures
  without deleting; `--yes` skips the confirmation prompt. The summary
  reports only the bytes actually measured, never a filesystem-delta
  guess, since other processes write to disk at the same time. `--history`
  lists past runs by date and bytes freed — `clean` frees space
  immediately rather than quarantining it, since fixing a full disk right
  now is the whole point, so this log is the only record of what a past
  run actually did.
- **dupes** — walks a directory tree (home by default), groups files by
  size and then by content hash, and reports every set of byte-identical
  files along with the space keeping just one copy would reclaim. By
  default it never deletes anything: duplicate photos, videos and
  documents are your own data, not a regenerable cache. `--hardlink` is
  the one opt-in action, and it destroys no data even if it's wrong —
  every path keeps working and keeps reading the same bytes, now sharing
  one inode instead of two. It confirms first, same as `clean`, unless
  `--yes` is set.
- **tools** — lists the companion tools vitals complements rather than
  reimplements (ncdu, gdu, dust, btop, htop, nvtop, jdupes, smartctl) and
  whether each is on PATH. `--install <name>` runs your platform's own
  package manager — brew, apt, dnf, pacman, winget, or scoop. vitals never
  ships its own installer, and always confirms before running one unless
  `--yes` is set.
- **explore** — hands off to gdu, then ncdu, then dust, whichever is
  installed first, for real interactive drill-down-and-delete. `disk` and
  `clean` diagnose and estimate; this is the tool built for browsing.
- **live** — hands off to btop, then htop, whichever is installed first,
  for a live interactive view. `vitals top` is the fallback when neither
  is installed.

```bash
vitals clean --dry-run
vitals clean --history
vitals dupes --root ~/Downloads --hardlink
vitals tools --install btop
vitals explore ~/Downloads
vitals live
```

## vitals gpu

Per-GPU VRAM, utilization, temperature, power and clocks, plus the
processes holding VRAM on NVIDIA, read through whichever vendor CLI is
already installed — `nvidia-smi`, `rocm-smi`, or macOS `ioreg` for Apple
Silicon's unified memory. Reports nothing, gracefully, when no supported
GPU is present. For a live per-process view, install nvtop:
`vitals tools --install nvtop`.

```bash
vitals gpu
vitals gpu --json | jq '.devices[] | {name, util_percent, temp_c}'
```

## Automation and integration

- **`vitals serve`** / **`vitals export`** — the full snapshot as
  Prometheus text-exposition metrics, following OpenTelemetry semantic
  conventions (`system_cpu_utilization`, and so on) plus a handful of
  vitals-specific signals (`vitals_llm_gpu_offload_ratio`,
  `vitals_verdict`). `serve` exposes `/metrics` on `127.0.0.1:9100` by
  default — loopback only, so `vitals serve` never opens a port other
  machines can reach until you ask it to. Pass `--addr :9100` (a bare
  `:PORT`, no host) to bind every interface instead, the way
  `node_exporter` does, once you actually want Prometheus or Grafana Agent
  on another host to scrape it. `export` is the one-shot form for a
  textfile collector.
- **`vitals mcp`** — speaks newline-delimited JSON-RPC 2.0 on stdin/stdout
  so an MCP client (Claude Code, Claude Desktop) can call vitals mid-task.
  Tools exposed: `system_health`, `diagnose_bottleneck`, `llm_status`,
  `gpu_status`. Register it with `claude mcp add vitals -- vitals mcp`.
- **`--webhook URL`** on `doctor` — posts the JSON envelope only when the
  verdict needs attention, so a cron job running `vitals doctor` can page
  a Slack channel or any webhook-based alerting tool with no extra glue.
  The URL must be `https`, and must not resolve to a loopback, private, or
  link-local address (this also blocks the cloud-metadata address,
  `169.254.169.254`) — both are refused before any request is sent, to
  close off using `--webhook` as an SSRF vector if its value ever comes
  from somewhere less trusted than your own command line (templated CI,
  say). Pass `--webhook-allow-insecure` to lift both restrictions for a
  local relay or a receiver you run yourself over plain http.
- **`--compare old.json new.json`** on `doctor` — diffs two saved reports:
  which findings appeared, which resolved, whether the verdict itself
  changed. Save the "before" state with `--output` ahead of a deploy or a
  suspected regression, then compare afterward.

## Configuration file

Alert thresholds have sensible defaults, but a build server that
legitimately idles at 90% CPU and a laptop at 90% CPU mean opposite
things. Every threshold that varies by machine role can be overridden in a
small config file:

```bash
# macOS: ~/Library/Application Support/vitals/config.toml
# Linux: ~/.config/vitals/config.toml (or $XDG_CONFIG_HOME/vitals/config.toml)
# Windows: %AppData%\vitals\config.toml

disk_warn_percent = 95              # default 90
disk_critical_percent = 99          # default 97
ram_warn_percent = 85               # default 78
ram_high_percent = 95               # default 90
cpu_oversubscribe_multiplier = 4    # default 2.0 — load1 >= this * cores triggers a finding
ollama_url = "http://gpu-box:11434" # default for --ollama-url when the flag is omitted
```

The format is a flat `key = value` list on purpose, not TOML or YAML — a
handful of numeric knobs don't justify a parsing dependency. `#` starts
a comment. A missing file, an unreadable file, or an unrecognized key is
never an error; vitals just falls back to its built-in default for
whatever isn't set.

### Extending memhogs

`memhogs` resolves most application families automatically — the macOS
`.app` bundle ID, the Linux systemd/flatpak/snap cgroup scope, the Windows
install directory — so most GUI apps are grouped correctly with no
configuration at all. The bundled family list only covers what the OS
can't express on its own: cross-app groups (Node, Python, JVM and Rust
toolchains, Electron) and headless daemons with no bundle (Postgres,
Redis, llama.cpp). Add your own, or override a built-in one, with a file
at the same config location as above:

```json
// <config dir>/vitals/families.json
[
  { "name": "My Internal Tool", "pattern": "(?i)my-daemon-name", "stop": "kill" }
]
```

`stop` is one of `kill`, `quit-app` (a macOS `osascript` quit, falling
back to a plain kill elsewhere), `pattern` (kill by matching the command
line, for multi-process apps like Firefox), or `docker-all` (stop every
Docker Desktop process).

## Shell completion

```bash
vitals completion bash | sudo tee /etc/bash_completion.d/vitals
vitals completion zsh  > "${fpath[1]}/_vitals"
vitals completion fish > ~/.config/fish/completions/vitals.fish
```

## Contextual help

```bash
vitals help              # command list
vitals help doctor       # full help for one command
vitals doctor -h         # the same, from the command itself
```
