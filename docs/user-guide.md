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
(cross-platform color and real terminal-width detection), plus a
Windows-only named-pipe dialer for the container page, are the
deliberate exceptions to "hand-write it instead." Runs the same way on
macOS, Linux and Windows.

**On this page** — `vitals guide --web` builds its own navigable table
of contents from the headings below automatically; this plain list is
for reading the raw file itself (GitHub, an editor, `vitals guide --raw`,
the plain `vitals guide` terminal view) without one:

- [Quick start](#quick-start)
- [Quick reference](#quick-reference)
- [vitals doctor](#vitals-doctor)
- [vitals view](#vitals-view)
- [vitals dashboard](#vitals-dashboard)
- [Resource deep dives: cpu, mem, disk, net, power](#resource-deep-dives-cpu-mem-disk-net-power)
- [vitals advice](#vitals-advice)
- [vitals heal](#vitals-heal)
- [vitals llm](#vitals-llm)
- [vitals top, memhogs, memcheck](#vitals-top-memhogs-memcheck)
- [vitals clean, dupes, tools, explore, live](#vitals-clean-dupes-tools-explore-live)
- [vitals gpu](#vitals-gpu)
- [vitals containers](#vitals-containers)
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
`cpu 4%  mem 74%  disk 59% (/)  net ↓12 KB/s ↑3 KB/s  battery 100%` —
whether the verdict is healthy or not, so "healthy" is something you can
check yourself instead of taking on faith, and a resource with nothing
wrong still shows a real number here rather than going unmentioned.

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

The `--json` payload carries a `schema_version` (currently `1.8.0`), and the
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

## vitals view

`view` is the whole machine on one terminal screen: `doctor`'s verdict
and findings at the top — in colour, the point of the screen — then
tiled CPU / memory / disk / network / power / GPU panels and the top
processes by CPU, sized to fit without scrolling. Running **bare
`vitals`** on an interactive terminal shows this too (piped or
redirected, bare `vitals` still prints the command list); `VITALS_VIEW=1
vitals` forces it into a pipe, e.g. `VITALS_VIEW=1 vitals | less -R`.

```bash
vitals            # on a TTY: the at-a-glance view
vitals view       # the same, explicitly
```

It's a **snapshot, not a live monitor** — one pass, then it returns
(exit 0/1/2 by verdict, same as `doctor`). For a live process view,
`vitals live` hands off to btop/htop; `vitals top` is the built-in
fallback. Panels for absent hardware (no battery, no GPU) simply don't
appear. On a very short terminal it trims the process rows first, then
whole panels — never the verdict or findings.

## vitals dashboard

`dashboard` serves the same correlation `doctor` prints in a terminal as
browsable pages instead. It binds `127.0.0.1` only and opens your default
browser automatically; nothing leaves this machine, the same promise
`vitals guide --web` makes.

```bash
vitals dashboard                # random port, opens your browser
vitals dashboard --addr :8080   # a stable port to bookmark — still 127.0.0.1 only
vitals dashboard --no-open      # print the URL instead of launching a browser
```

A grouped sidebar organises the pages into **Overview**, **Resources**
(CPU, Processes, Memory, Disk, Network, Power, GPU), **Intelligence**
(Advice, LLM Insight), **Tools** (Clean, Duplicates) and **System**.
Each page only appears when this machine can actually offer it: no GPU
page without a GPU, no Power page without a battery.

Every resource page carries a "what's using it" section, appropriate to
that resource:

- **CPU / Memory** — the top processes by CPU% and by resident memory.
- **Processes** — the full `vitals top` table as a page.
- **Disk** — the biggest directories and biggest single files under your
  home folder, from a read-only scan shared with `vitals disk --scan`.
  Visual donut charts display space distribution across top folders and files.
  Results are cached up to 10 minutes (64-entry LRU), so navigating up and down
  the directory tree or returning to the page uses cached data without rescanning.
  Directory drill-down, breadcrumbs/up navigation and rescan controls
  narrow the scope; loading, access errors and partial lower-bound sizes
  are visible rather than hidden. The page renders before scanning.
  Logical file sizes are not promised recoverable physical storage.
  On Linux and Windows, also the top processes
  by live disk I/O rate. That per-process rate isn't available on macOS,
  so the section is simply absent there rather than showing zeros.
- **Network** — the active connections: which process is talking to
  which remote host, and in what state. (Per-process bandwidth — who's
  moving the most bytes — isn't exposed by the OS without extra
  privileges, so it isn't shown.)
- **Power** — on macOS, a real per-process "energy impact" table using
  the same power score Activity Monitor shows (no `sudo` needed). On
  Linux and Windows, where no equivalent no-privilege number exists, a
  CPU-ranked list labelled plainly as a CPU-based estimate.
- **GPU** — processes holding VRAM where that reading is available
  (NVIDIA), or the memory pressure itself on an Apple unified-memory GPU.
- **Containers** — appears when any local runtime is detected, with a
  section per runtime (Docker *and* Kubernetes together on a kind/k3s
  host): each container/pod grouped by compose project / namespace, with
  its ports, short ID, uptime, and per-container CPU and memory against
  its limit. There's also a Containers card on the Overview. See
  [vitals containers](#vitals-containers).

**LLM Insight** shows local runtimes and their per-model GPU offload
alongside cloud-provider reachability. **System** shows the machine
identity, the effective config (with each value marked as set-in-file or
a built-in default), and which companion tools are installed.

The Advice page is always available — its rule-based findings need no
LLM at all; an LLM, when reachable, only adds AI commentary on top (see
[vitals advice](#vitals-advice)). Press Ctrl+C in the terminal that
launched it to stop.

Every read-only page refreshes itself in place every ten seconds, so a
dashboard left open on a second monitor stays current without a reload.
It only polls while the tab is actually visible, and the top bar carries
an **auto-refresh: on/off** toggle (the choice is remembered per
browser) for when you want the numbers to hold still. The Clean and
Duplicates pages don't auto-refresh — they hold the result of a Preview
you ran, which a refresh would wipe.

The dashboard follows your system's light/dark setting; the sun/moon
button in the top bar overrides it, and that choice is also remembered
per browser. Both themes are built to WCAG 2.2 AAA: body and status text
hold at least 7:1 contrast, every control is at least 44×44 px, the
first Tab stop is a "Skip to content" link, and animations stop when
your OS asks for reduced motion. Everything — styles, icons, scripts —
is served by the binary itself, including its two typefaces (Space
Grotesk and IBM Plex Mono, SIL Open Font License) and its Lucide icons
(ISC), with every licence text embedded alongside them and readable at
`/_static/LICENSE-*.txt` on the running dashboard — so it renders the
same with no network.

The clean page mirrors `vitals clean` for the browser: select categories
and a temporary-file minimum age, then **Preview** without filesystem
mutation. Caches and old temporary files are selected by default; logs,
Trash and tool commands require explicit selection. Only a complete preview
reveals **Apply**; changing selection requires another preview. Commands
are listed with their consequences and unknown reclaim, not a false byte
estimate. Apply asks for confirmation and rescans the selected categories
(the preview is not a frozen file list), then reports each action's outcome.
It performs the same selective cleanup `vitals clean` does — this
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
  with projected time-to-full, and a default cache/temp cleanup-candidate
  estimate in logical bytes — the same scope `vitals clean` would touch,
  actually measured and capped to
  a short time budget so a huge cache can't stall the command. When
  `smartctl` (smartmontools) is installed, each physical disk's
  S.M.A.R.T. pass/fail, temperature, and — on NVMe — the percentage of
  rated write life used are shown too; a FAILED verdict or wear at/over
  90% raises a finding. macOS and Linux only for now. On Linux and
  Windows, a "top processes by disk I/O" table ranks processes by their
  live read+write rate over the sample window; macOS has no per-process
  I/O counter, so that table is omitted there.
- **net** — per-interface rx/tx, the top remote peers by established TCP
  connection count, and a DNS-resolution latency check, which tells apart
  "DNS is slow" from "the link is slow" without needing raw-socket
  privileges. (Per-process bandwidth isn't shown — the OS doesn't expose
  per-process byte counts without extra privileges.)
- **power** — battery charge, the OS runtime estimate, health versus design
  capacity, charge direction (draining while plugged in is its own
  finding), and macOS Low Power Mode state, which changes performance
  expectations and is otherwise invisible. On macOS, an "energy impact by
  process" table using the same no-`sudo` power score Activity Monitor
  shows.

All five take `--json`, `--output FILE`, `--ci`, and `--quiet`/`-q`, same as
`doctor`.

```bash
vitals disk                              # per-mount health and cleanup candidates
vitals disk --scan                       # recursive space consumers under home, read-only
vitals disk --scan --root "$HOME/Library" --top 20 --scan-budget 1m
vitals disk --scan --json | jq '.disk_usage.directories'
vitals cpu --json | jq '.snapshot.cpu.per_core_percent'
vitals net --ci                          # one line, for a log
vitals power -q; echo $?                 # exit code only
vitals mem --output mem-snapshot.json
```

`disk --scan` defaults to 20 rows and a 30-second cooperative scan budget.
`--root`, `--top` (1–100) and `--scan-budget` (>0, at most 5 minutes)
require `--scan`; `--quiet` and `--ci` cannot hide a requested scan.
The budget applies to scanning after telemetry collection and is checked
between filesystem operations; it cannot interrupt a blocked OS call.
Incomplete scans exit at least 1, root/scan failures exit 2, and an existing
critical health finding still exits 2. With `--scan`, JSON stdout and
`--output` carry the same resource-tagged envelope and `disk_usage` field;
without it, existing output shapes are unchanged.

Directory rows are recursive logical bytes; the separate file rows overlap
those totals, so **do not add the tables together**. Partial totals are
lower bounds and rankings mean "largest observed", not necessarily the
true largest. Symlinks, special files and detected nested mounts are skipped;
unavailable mount-boundary detection is stated explicitly. Home-only scans
do not account for other users, system data or snapshots. Hardlinks, sparse
files and filesystem clones/compression mean these numbers are not exact
allocated bytes or guaranteed reclaim. Drill down first; use
`vitals clean --dry-run` for supported caches, and the owning application's
storage management for models/VMs rather than deleting individual blobs.
A healthy disk verdict means no configured threshold was exceeded, not
that your next workload has enough free space.

## vitals advice

`advice` answers in two parts, and the first one needs no LLM at all:
`doctor`'s own rule-based findings and fixes print immediately, exactly
as `vitals doctor` would show them — including the same at-a-glance
`cpu · mem · disk · net · battery` line, so it's clear every resource
was actually checked even when only one of them has a finding worth
raising; a resource with nothing wrong shows a healthy number here
instead of just going unmentioned. Then, if a local or cloud LLM is
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

`advice` can also incorporate a deep disk usage scan into the prompt and commentary
via `--scan` (supporting `--root`, `--scan-budget`, and `--top`), highlighting the
largest space consumers when disk capacity is tight. On the dashboard, the Advice page
automatically incorporates recent cached disk scans into its context. Memory
diagnostics are machine-capacity and memory-pressure aware — distinguishing true
leaks from normal KV cache, attention buffer, and allocator retention in runtimes
like `llama-server` or database servers, advising context/cache limits or graceful
restarts instead of blind kill commands when host memory is healthy.

```bash
vitals advice
vitals advice --scan                                      # include deep disk usage in prompt & advice
vitals advice --scan --root "$HOME/Library" --top 10      # scoped scan
vitals advice --provider ollama --model llama3.1:8b
OPENAI_API_KEY=sk-... vitals advice --provider openai
vitals advice --provider lmstudio --lmstudio-url http://localhost:1234
vitals advice --json | jq -r .heuristic_advice   # always present, no LLM required
vitals advice --json | jq -r .llm_advice         # present only when a provider answered
vitals advice --scan --json | jq -r .disk_usage.directories # disk scan data in JSON payload
```

`--json`'s `source` field is `"heuristic"` or `"heuristic+llm"`, and
`llm_error` is set instead of `llm_advice` when no provider answered —
the payload always has something to show, it just tells you honestly
which half of it is present.

## vitals heal

`heal` is the third step: `doctor` diagnoses, `advice` explains, `heal`
**acts** — it runs the machine-executable fix a finding carries, after
showing you the exact command and asking. It re-checks the machine
itself first (a fast pass, no probes, no history write), so it acts on
what's true now, not on a report from earlier.

It runs in an **interactive terminal only** — a piped or scripted run
refuses (`--dry-run` works anywhere and changes nothing). The v1 remedy
set is deliberately tiny: `sudo purge` (macOS — frees the drop-on-demand
file cache) for the "RAM high (likely reclaimable)" finding, and a
`vitals clean` delegate for a "disk nearly full" / "out of inodes"
finding. The delegate runs `vitals clean --dry-run` first, shows it, and
asks again before the real cleanup — so `clean`'s own preview,
confirmation and audit history all still apply. Anything else prints its
fixes and does nothing.

```bash
vitals heal --dry-run              # show every remedy and its risk; run nothing
vitals heal                        # step through each, confirming one at a time
vitals heal --only disk-low        # just the remedy for that finding id (see `vitals doctor --json`)
vitals heal --yes                  # pre-answer the prompt (still needs a real terminal)
```

Every finding in `vitals doctor --json` now carries a stable `id`, and a
finding with an automatable fix carries a `remedy` object (`kind`,
`argv`, `risk`, `reversible`). `heal` will only ever run `sudo purge` or
a `vitals` subcommand — it refuses anything else even if a crafted
report somehow reached it. Note that `sudo` caches your password for a
few minutes, so a second `heal` in quick succession may not re-prompt.

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

**Cleanup is at your own risk.** Back up important data and close affected
applications before applying. Deletion is permanent: no quarantine,
rollback or undo is provided. A preview or a file's age cannot guarantee
that it is unused. Opt-in controls and preservation checks reduce risk;
they do not eliminate it. CLI, JSON (`warning`) and dashboard output carry
this warning, including the final confirmation.

The disk-cleanup family, from "reclaim space right now" to "let me look
around and decide myself":

- **clean** — defaults to developer/OS caches and temporary files older
  than seven days. **Changed default:** logs, Trash, package-manager
  cleanup and resource-pruning commands are no longer swept automatically.
  Use `--categories caches,temp,logs,trash,tools` to explicitly select
  categories; selection replaces, rather than extends, the default.
  `--yes` only skips confirmation, never expands selection.
  `--min-age 168h` controls temporary-file eligibility, not caches, logs
  or Trash. Close affected applications: age does not prove a file is
  unused, and removing caches may require downloads or rebuilds.
  `--dry-run` shows the shared plan without deletion or command execution;
  `--json` exposes action estimates, completeness, preserved-file counts,
  outcomes and issues (applying JSON mode requires `--yes`).
  Overlapping paths are counted once; differing overlapping categories
  use conservative temporary-file retention. Symlinks, non-regular files,
  changed files and directory structures are preserved. Scans check
  cancellation inside traversal, cap at 100,000 entries and 30 seconds
  (three seconds for browser previews); incomplete targets are never
  cleaned. Preview again with fewer categories if a limit is reached.
  Tool commands are opt-in, show their consequences and unknown reclaim,
  and have a two-minute timeout per command. Missing tools and failures
  are reported, not silently ignored. `--tools uv,pnpm` restricts the
  tool selection; it requires `tools` in `--categories`. With no subset,
  all installed supported tools are selected. Windows Prefetch is no longer purged.
  Package-manager paths are discovered with bounded, read-only native
  queries (`npm config get cache`, `pnpm store path`, `pip cache dir`,
  `uv cache dir`, `go env GOCACHE`, `brew --cache`, `conda info --json`,
  and NuGet's cache-list command). The plan includes the discovery
  command and returned locations. `pip3` is used when `pip` is absent.
  Failed or malformed discovery skips that action with an explanation;
  it never falls back to a guessed package-manager folder.
  Direct archive-cache targets honor `GRADLE_USER_HOME`, `CARGO_HOME`
  and `XDG_CACHE_HOME`, with their documented defaults when unset.
  OS paths derive from the current home and OS environment rather than
  a particular user's absolute paths; Windows browser discovery includes
  all cache-bearing Chrome/Edge/Firefox profiles. Managers without a
  verified location query remain native-command-only, with unknown reclaim.
  Apply rescans: an earlier preview is not a frozen authorization of
  specific files. Results credit only successfully removed logical file
  bytes, not guaranteed physical space recovery (hardlinks, snapshots and
  concurrent writes affect that). Free-space change is context only.
  Partial failures produce a nonzero CLI exit and structured issues;
  browser results show the same issues. `--history` lists past runs
  as readable text and cannot be combined with `--json`;
  the audit log also records action outcomes, including failures. Cleanup
  is immediate, not quarantine or rollback.

  Coverage is cross-platform and workload-based, not a scan of one
  developer's machine:

  | User/workload | Covered cleanup | Deliberately preserved |
  | --- | --- | --- |
  | Everyday desktop / office | OS/application caches; Windows Chrome/Edge/Firefox cache targets; old temporary files where available | Documents, browser profiles, passwords, downloads; Trash is opt-in |
  | JS / Python developers | npm, pnpm, pip and uv native cache discovery and cleanup | Source trees, dependencies inside projects; known manager-owned cache trees are not manually swept |
  | Go / Java / Rust / .NET developers | Go build/test cache command; environment-aware Gradle cache targets and Cargo downloaded archives; NuGet HTTP cache command | Installed SDKs/tools, Go modules, NuGet global packages, project outputs; Maven/Yarn are not guessed |
  | Data science / local AI | Conda archive/index cleanup without package-cache or environment removal; common model/dataset cache names and model file extensions are protected | Conda environments, extracted Conda packages, known model/dataset/checkpoint trees |
  | Container / platform operators | Explicit Docker prune; apt/dnf/yum/pacman, Flatpak, journal, Homebrew, unavailable simulators, DISM as platform-appropriate | Docker volumes; pruning stopped containers and downgrade/history copies requires explicit tool selection |
  | Creators / gamers | Ordinary OS cache/temp cleanup | Known Adobe/Blender/game cache trees, autosaves and backups are conservatively protected; no speculative asset/save cleanup |

  Protected-name rules are conservative safeguards, not a universal
  classifier for every application's data. Unlisted custom data stored
  in cache directories can still be selected: review the plan and use
  `vitals explore` for large assets, models or game libraries rather
  than treating “unused” as permission to delete.
  uv must manage its own cache; `uv cache prune` can also remove
  centralized project environments, which are recreated when needed.
  Conda uses only `--tarballs --index-cache --yes`, never `--all` or
  `--force-pkgs-dirs`. Go does not clear `-modcache`; NuGet clears only
  `http-cache`, not `global-packages`.
- **dupes** — walks a directory tree (home by default), groups files by
  size and then by content hash, and reports every set of byte-identical
  files along with the space keeping just one copy would reclaim. By
  default it never deletes anything: duplicate photos, videos and
  documents are your own data, not a regenerable cache. `--hardlink` is
  the one opt-in action, and it destroys no data even if it's wrong —
  every path keeps working and keeps reading the same bytes, now sharing
  one inode instead of two. It confirms first, same as `clean`, unless
  `--yes` is set. `--fast` uses `jdupes` as the backend when it's
  installed — much faster on a large tree; it reports no scanned-file
  total (jdupes doesn't expose one) and vitals re-applies its own
  skip-list (`.git`, `node_modules`, caches, trash) to the results. If
  `jdupes` isn't installed or errors, the built-in scan runs instead.
- **tools** — lists the companion tools vitals complements rather than
  reimplements (ncdu, gdu, dust, btop, htop, nvtop, jdupes, smartctl) and
  whether each is on PATH. `--install <name>` runs your platform's own
  package manager — brew, apt, dnf, pacman, winget, or scoop. vitals never
  ships its own installer, and always confirms before running one unless
  `--yes` is set.
- **explore** — hands off to gdu, then ncdu, then dust, whichever is
  installed first, for real interactive drill-down-and-delete. `disk` and
  `clean` diagnose and estimate; `disk --scan` and dashboard drill-down are
  read-only summaries, while companions offer a full interactive explorer.
- **live** — hands off to btop, then htop, whichever is installed first,
  for a live interactive view. `vitals top` is the fallback when neither
  is installed.

```bash
vitals clean --dry-run
vitals clean --categories caches --dry-run --json
vitals clean --categories trash --dry-run  # explicitly review permanent Trash removal
vitals clean --categories temp --min-age 336h --dry-run
vitals clean --categories tools --tools uv,pnpm,conda --dry-run
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

## vitals containers

Surfaces a locally-running container runtime or Kubernetes when one is
present — the same "only shown when it's there" gating as the GPU page.
It is strictly read-only and never manages anything: every suggested fix
is a `docker` or `kubectl` command for you to run.

- **Docker / Docker-API-compatible runtimes** (Docker Desktop, Colima,
  Podman's compat socket, Rancher Desktop) are reached over their local
  socket — a unix socket on macOS and Linux, the `docker_engine` named
  pipe on Windows — with only `GET` requests against a fixed endpoint
  list and no vendored Docker client.
- **A local Kubernetes** (kind, k3s, minikube, Docker Desktop's built-in
  k8s) is read via `kubectl` — but only when your current context points
  at a loopback / private-range API server. A context aimed at a cloud
  cluster is ignored entirely: vitals makes no network call and touches
  no credentials.

**Both runtimes at once.** A kind/k3s/minikube host runs a Docker daemon
*and* a Kubernetes API — so `vitals containers` shows every reachable
runtime, each as its own section (`DOCKER · /var/run/docker.sock`, then
`KUBERNETES · kind-kind`). The dashboard's Containers page and its
Overview card do the same. `--runtime docker` / `--runtime kubernetes`
narrows the output to one.

Each container/pod shows its state, published ports, short ID, uptime,
compose project / namespace grouping, and — sampled on demand —
per-container CPU and memory against its limit. Findings cover OOM-kills,
`CrashLoopBackOff` / `ImagePullBackOff`, restart loops, and failing
healthchecks — and, when the machine is already tight on RAM, that the
runtime's own VM is holding memory the host process list can't see.
`doctor --json` carries a cheap list under `snapshot.containers` (an
array — one entry per runtime; no per-container stats on that path, so it
stays fast).

```bash
vitals containers
vitals containers --runtime kubernetes           # just the pods on a kind/k3s host
vitals containers --json | jq '.snapshot.containers[].containers[] | select(.oom_killed)'
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
ollama_url = "http://gpu-box:11434"   # default for --ollama-url when the flag is omitted
lmstudio_url = "http://gpu-box:1234"  # default for --lmstudio-url (vitals advice only)
llamacpp_url = "http://gpu-box:8080"  # default for --llamacpp-url (vitals advice only)
vllm_url = "http://gpu-box:8000"      # default for --vllm-url (vitals advice only)
```

The four URL overrides have no built-in default of their own — vitals
already probes each runtime's well-known local port
(`http://localhost:11434`/`1234`/`8080`/`8000`) with no config at all; set
one here only to point at a different host (a GPU box on the LAN) or
port.

The format is a flat `key = value` list on purpose, not TOML or YAML — a
handful of numeric knobs don't justify a parsing dependency. `#` starts
a comment. A missing file, an unreadable file, or an unrecognized key is
never an error; vitals just falls back to its built-in default for
whatever isn't set.

The first plain `vitals doctor` run on a machine with no config file
**creates one for you**, with every threshold written out at its current
default — nothing commented out, so changing one is a single-line edit.
Because the written values *are* the defaults, the file's existence alone
changes nothing until you edit a value. `vitals info` shows the file's
path, which values are still at their default, and which you've changed.
(The `ollama_url` line is the one commented-out example, since it has no
default — uncomment it to set one.)

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
