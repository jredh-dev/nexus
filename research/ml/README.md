# ml

Go ports of ML algorithms from [mathews-tom/no-magic](https://github.com/mathews-tom/no-magic). Single-file, zero-dependency implementations — train and infer, stdlib only.

## Stop here if...
- You're looking for production ML inference — this is educational/research code
- You want the Python originals — see the upstream repo

## What's here

All files use the `research` build tag and are excluded from default compilation.

| Algorithm | File | Status |
|-----------|------|--------|
| BPE Tokenizer | `microtokenizer.go` | Done |
| Flash Attention | `microflash.go` | Done |
| Scalar Autograd | `value.go` | Done |
| GPT (char-level) | `microgpt.go` | Done |
| Quantization | `microquant.go` | Done |

## Run

```bash
go test -tags research -v ./research/ml/
go test -tags research -bench=. -benchmem ./research/ml/
```

To run demos interactively, call `ml.RunMicrotokenizer()`, `ml.RunMicroflash()`, `ml.RunMicrogpt()`, or `ml.RunMicroquant()` from a tagged main or test.

## Design

- **Library package, not a binary.** `package ml` with exported functions.
- **Build-tagged.** `//go:build research` — invisible to `go build ./...` and `go test ./...`.
- **Zero dependencies.** stdlib only. No `gonum`, no BLAS bindings.
- **Train and infer.** Full lifecycle in every algorithm.
- **Comments match the Python originals.** Same signpost style, same algorithm references.

Attribution: algorithms and educational structure from [no-magic](https://github.com/mathews-tom/no-magic) (MIT). Go ports are AGPL-3.0.
