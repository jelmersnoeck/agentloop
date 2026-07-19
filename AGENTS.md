# AGENTS.md

Guidance for agents and contributors working in this repository. (`agentloop` itself reads
`AGENTS.md`/`CLAUDE.md` files — this one dogfoods that format while documenting the project.)

## What this is

`agentloop` is a provider-agnostic Go SDK for building agents: a command-in/event-out loop over
a pluggable LLM provider, plus tools, sub-agents, agent groups, `AGENTS.md` config, and
cross-provider caching. It is a reusable base for building many different agents, not an agent
itself.

- **Module:** `github.com/jelmersnoeck/agentloop`
- **Go version:** 1.24+
- **Status:** early development; built as a 5-milestone sequence (see the roadmap in
  `README.md` and the plans under `docs/superpowers/plans/`).

## Architecture invariant (do not break)

`llm` is the dependency-free contracts package. **It MUST NOT import any other package in this
module.** Every other package (`tool`, `subagent`, `agentgroup`, `agentsmd`, `cache`, and the
root `agentloop` package) depends on `llm`, never the reverse, and leaf packages do not import
each other's internals. This is the load-bearing decision that keeps the SDK decoupled — a
change that adds an in-module import to `llm` should be rejected.

## Conventions

- **TDD.** Write the failing test first, watch it fail, then implement the minimal code to pass.
- **Streaming is the only provider mode.** `Provider.Stream(ctx, req) (<-chan llm.Event, error)`;
  non-streaming callers drain the channel.
- **`Context` is serializable.** Keep `llm.Context{System, Messages, Tools}` round-trippable —
  persistence, mid-run model switching, and sub-agent transfer depend on it.
- **Thinking blocks carry a `Signature` that must be preserved verbatim** and replayed to the
  provider; never strip it.
- **Determinism for prompt caching.** Keep tool ordering stable (sorted), JSON serialization
  canonical, and volatile content (dates, live status) out of the cacheable prefix.
- **Concurrency.** The loop, mock, and future sub-agents use goroutines/channels; run
  `go test -race ./...` on anything touching them.

## Design source of truth

The full design and rationale (loop, steering/queues, tools, sub-agents, routing, caching,
convergence) live in `docs/superpowers/specs/2026-07-19-agentloop-sdk-design.md`. Milestone
plans live in `docs/superpowers/plans/`. Consult these before making structural changes.

## Verifying changes

Before committing, all of the following must be clean:

```bash
go build ./...
go test ./...
go test -race ./...   # for concurrency-touching changes
go vet ./...
gofmt -l .            # must print nothing
```

## Deferred items (tracked for later milestones)

The minimal loop (milestone 1) intentionally defers to the milestone-2 loop rewrite: making
`Agent.emit` context-aware, closing the `Events()` channel on run completion, and preserving
partial assistant text on the error path. Don't treat these as bugs to fix in isolation — they
are resolved structurally when the full loop (steering queues, hooks, cancellation) lands.
