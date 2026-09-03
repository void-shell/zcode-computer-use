#!/usr/bin/env node
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const fixturePath = process.argv[2] ?? path.join(root, 'benchmarks', 'textedit-export.json')
const benchmark = JSON.parse(readFileSync(fixturePath, 'utf8'))

const rows = benchmark.traces.map(trace => {
  const bytes = trace.calls.reduce((sum, call) => sum + Buffer.byteLength(JSON.stringify(call)), 0)
  return {
    ...trace,
    turns: trace.calls.length,
    bytes,
    estimatedTokens: Math.ceil(bytes / 4)
  }
})

console.log(`# One-task turns/tokens benchmark: TextEdit PDF export\n`)
console.log(`**Task:** ${benchmark.task}\n`)
console.log('| Backend | Trace | Tool turns | Tool-call bytes | Estimated text tokens | Image observations |')
console.log('| --- | --- | ---: | ---: | ---: | ---: |')
for (const row of rows) {
  console.log(`| ${row.backend} | ${row.kind} | ${row.turns} | ${row.bytes} | ${row.estimatedTokens} | ${row.imageObservations} |`)
}
console.log(`\n## What this measures\n`)
console.log('This is a deterministic **protocol-path benchmark**, not an agent trial. Each row reaches the same scripted exit condition, and one tool request plus its result counts as one turn. The text-token column is the declared model-independent estimate `ceil(compact JSON request bytes / 4)`. It excludes tool results, system prompts, reasoning, and image tokens; image observations are therefore reported separately instead of converted with a vendor-specific formula. Coordinates in the screenshot baseline are fixture coordinates, not a claim that they transfer between machines.\n')
console.log('The result supports one narrow statement: the semantic menu/element path needs fewer tool round trips and image observations in this script. It does **not** establish an end-to-end token or task-success advantage. That requires paired runs with the same model, prompt, display, and successful output verification.\n')
console.log('## Reproduce\n')
console.log('```sh')
console.log('node scripts/render-benchmark.mjs')
console.log('```')
console.log('\nThe source trace is [`benchmarks/textedit-export.json`](../benchmarks/textedit-export.json).')
