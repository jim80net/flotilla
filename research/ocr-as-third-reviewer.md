# OCR (Alibaba open-code-review) as a third PR reviewer — benchmark & verdict

**Status: SHELVED (do not re-run this benchmark).** Verdict date: 2026-06-08.
Decision owner: operator (via XO). This note exists so no future session
re-spends host compute re-deriving it — read this first.

## Question

Could [alibaba/open-code-review](https://github.com/alibaba/open-code-review)
(OCR) serve as a **third** automated PR reviewer alongside cubic + the
`/systems-review` skill, at **$0 marginal cost** (i.e. driven by a local model,
not a paid API)?

## TL;DR verdict — SHELVE

- **$0 / local path is NOT viable on this host.** OCR is a standalone Go agent
  that makes its own LLM calls; the "Claude Code skill/plugin" is just a launcher
  that shells `ocr review` — it does NOT route through the Claude Code
  subscription (the Max OAuth token is not exposed as `ANTHROPIC_AUTH_TOKEN`,
  which is the only env OCR's "Claude Code environment" resolver reads). So the
  only $0 option is a local OpenAI-compatible model.
- Benchmarked against a local **Qwen2.5-Coder-32B-Instruct Q4_K_M** on the GB10
  (124 GB unified): **too slow + GPU-saturating + missed the known bug.**
- **Paid path** (Anthropic / cheap OpenAI-compatible) would be fast + better
  quality, but is **costly** (~208K input tokens per file, below) **and
  redundant** — cubic (Claude-grade) + `/systems-review` (Claude-grade) already
  cover that tier and *did* catch the bug OCR missed.

## Measurements (all measured 2026-06-08 on host rt-dgx-sp001, GB10)

Setup: `ocr` built from source on aarch64 (`v0.0.0-a32b8c7`); model served by
`llama.cpp` `llama-server` (build 8269); OCR → local endpoint via the OpenAI
protocol (`use_anthropic:false`); `ocr llm test` passed. (Aside: this llama.cpp
build lacks HTTPS, so `llama-server -hf` auto-download fails — the GGUF was
curl'd manually and served with `-m`.) OCR's deterministic file-selection
sensibly picked the **8 Go source files**, excluding docs (`.md`) and **all
`*_test.go`** (a coverage choice — cubic / `/systems-review` *do* consider tests).

| Metric | Result |
|---|---|
| **Speed — real PR** (8 Go files, +1141, the #18 idle-context-reset diff) | **DNF** — hit a 30-min cap with **zero output** (`--audience agent` emits only a final summary, so a mid-run timeout yields nothing) |
| **Speed — single 110-line file** (`internal/watch/clear.go`) | **DNF** at OCR's default **10-min/file** internal timeout on first run; **completed in 7m23s** on a retry with `--timeout 25` (agentic round-trip count varies run-to-run) |
| **Tokens — single 110-line file** | **~208,307 input** + ~3,448 output (the agent re-reads the file + codebase-search + a >50-line "plan" phase + positioning + reflection) |
| **Quality — single file** | **0 comments — "Looks good to me."** The file was the *buggy* `clear.go` (pre-fix commit `7f1e79e`) containing a known silent-failure (a pre-clear `Capture` error masking a Remote-Control drop) that `/systems-review` caught. **OCR-local MISSED it.** |
| **Host impact — GPU** | **pinned 93-96%** for the entire duration of every run (verified by 5-second sampling: 340/356 samples ≥93%). A review run would **saturate the production GPU** and disrupt concurrent work. |
| **Host impact — memory** | RAM **40 → 51 GB peak** (18.4 GB model + KV-cache growth). `nvidia-smi` cannot report VRAM on the GB10 (unified memory) → RAM is the proxy. Load ~2.0-2.5 (CPU modest). |

Comparison point: cubic reviews a whole PR in **~2-20 min** and `/systems-review`
runs Claude-grade in minutes; both caught the `clear.go` silent-failure.

## Fairness caveats (why this is "shelve," not "OCR is bad")

- The single-file result is from **isolated single-file mode** — OCR's full
  cross-file mode (its differentiator) **DNF'd** at this model/host tier, so its
  context-aware engine never got to run to completion.
- The model is a **local 32B**, not Claude-grade. A stronger model would likely
  be faster-relative-to-quality and might catch the bug — **but the whole point
  of the $0 path was a local model.** At the only $0 tier available, it failed.
- The integration plumbing all WORKED (build, `llm test`, file selection, server
  200s). The bottleneck is purely **model throughput × OCR's many-round-trip
  agentic loop** (and that loop's ~208K-tok/file appetite).

## If revisited (conditions that would change the verdict)

Only reopen if ALL of: (a) a materially faster local code model with reliable
multi-turn tool-calling exists on this host, AND (b) OCR's full cross-file mode
completes a real PR in a few minutes without saturating the GPU, AND (c) it
demonstrates findings cubic + `/systems-review` *miss*. Absent (c), it is
redundant regardless of speed. Do not re-benchmark without a new model that
plausibly clears (a)+(b).

## Artifacts

- The 18.4 GB GGUF (`Qwen2.5-Coder-32B-Instruct-Q4_K_M.gguf`) is **kept** at
  `/home/jim/models/` — dead weight for OCR, but a useful general local code
  model; non-pressing on a 124 GB host (operator's call to keep).
- OCR repo clone used for research: `/tmp/ocr-research-2975902` (ephemeral).
