# One-task turns/tokens benchmark: TextEdit PDF export

**Task:** Export the open TextEdit document as benchmark.pdf and verify that the save sheet closed.

| Backend | Trace | Tool turns | Tool-call bytes | Estimated text tokens | Image observations |
| --- | --- | ---: | ---: | ---: | ---: |
| zcode-computer-use | scripted protocol path | 7 | 471 | 118 | 1 |
| Anthropic-style screenshot loop | scripted primitive baseline | 10 | 657 | 165 | 5 |

## What this measures

This is a deterministic **protocol-path benchmark**, not an agent trial. Each row reaches the same scripted exit condition, and one tool request plus its result counts as one turn. The text-token column is the declared model-independent estimate `ceil(compact JSON request bytes / 4)`. It excludes tool results, system prompts, reasoning, and image tokens; image observations are therefore reported separately instead of converted with a vendor-specific formula. Coordinates in the screenshot baseline are fixture coordinates, not a claim that they transfer between machines.

The result supports one narrow statement: the semantic menu/element path needs fewer tool round trips and image observations in this script. It does **not** establish an end-to-end token or task-success advantage. That requires paired runs with the same model, prompt, display, and successful output verification.

## Reproduce

```sh
node scripts/render-benchmark.mjs
```

The source trace is [`benchmarks/textedit-export.json`](../benchmarks/textedit-export.json).
