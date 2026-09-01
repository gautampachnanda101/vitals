---
description: Run vitals doctor and explain the verdict — what's constraining this machine and the fix.
---

Run `vitals doctor --json` and interpret it for me:

1. State the overall `verdict` and `exit_code`.
2. Walk the `findings` most-severe-first: for each, give the `title`, the
   `detail`, and its `fixes`.
3. Recommend the single highest-impact action.
4. Do **not** execute any `kill` / `pkill` / `docker` / `purge` / `clean`
   command — show it and let me run it.

If `vitals` isn't installed, tell me how to install it and stop.

$ARGUMENTS
