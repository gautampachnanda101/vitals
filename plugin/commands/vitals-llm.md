---
description: Inspect local and cloud LLM endpoints, per-model GPU offload, and quant fit.
---

Run `vitals llm` and summarise:

1. Which local runtimes (Ollama, LM Studio, llama.cpp, vLLM) are up, and which
   cloud providers are reachable.
2. For each loaded model, its GPU offload % — flag anything below 100% as a
   generation-speed problem and suggest a smaller quant or fewer layers.
3. If I named a model in the arguments, also run `vitals llm fit <model>` and
   tell me the largest quant that fully fits this machine's VRAM.

Never print or echo API keys.

Model (optional): $ARGUMENTS
