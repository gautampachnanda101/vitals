# vitals — Claude Code plugin

Wraps the [`vitals`](https://github.com/gautampachnanda101/vitals) binary for
Claude Code.

## What's in it

- **Skill `system-diagnostics`** — auto-triggers when you tell Claude the machine
  is slow / frozen / out of memory / disk-full / the LLM is crawling. Claude runs
  `vitals doctor --json`, leads with the top finding, and hands you the fix
  (never runs a destructive command itself).
- **Slash commands** — `/vitals-doctor`, `/vitals-llm [model]`, `/vitals-clean`.
- **Optional session pre-flight** — a `SessionStart` hook that runs
  `vitals doctor` and surfaces a warning if the machine is unhealthy. **Off by
  default**; enable it with `export VITALS_PREFLIGHT=1` in your shell profile.

## Install

The plugin needs the `vitals` binary on `PATH`:

```sh
brew install gautampachnanda101/tap/vitals
# or
curl -fsSL https://raw.githubusercontent.com/gautampachnanda101/vitals/main/install.sh | sh
```

Then add this plugin to Claude Code (from a marketplace that lists it, or by
pointing Claude Code at this directory).

## Prefer the MCP server?

If you only want Claude to *call* vitals (no skill / commands / hook), skip the
plugin and register the MCP server instead:

```sh
claude mcp add vitals -- vitals mcp
```

Tools: `system_health`, `diagnose_bottleneck`, `llm_status`, `gpu_status`.
