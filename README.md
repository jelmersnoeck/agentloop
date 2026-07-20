# agentloop

A reusable, provider-agnostic Go SDK for building agents.

At its core, `agentloop` is the loop that takes instructions, talks to a pluggable LLM
provider to make decisions, and executes what the model asks for — plus the surrounding
machinery (tools, sub-agents, agent groups, `AGENTS.md` config, cross-provider caching)
that every agent otherwise re-implements. It is designed to be a solid base you build many
different agents on top of.

> **Status: early development.** The foundation (Plan 1 of 5) has landed: the `llm` contracts
> package, a scriptable mock provider, and a minimal blocking `Run` loop. Real
> providers, tools, sub-agents, and caching are being built in subsequent milestones — see
> [Roadmap](#roadmap). APIs will change until a tagged release.

## Why

An independent agent loop — not shaped by any single vendor's SDK — lets you mix providers
freely: run an orchestrator on a strong reasoning model, fan work out to cheap-and-fast
sub-agents, and even switch models per message. The three big provider SDKs each impose their
own worldview; `agentloop` sits one layer above them and stays neutral.

## Design principles

- **Extensibility first.** Providers, tools, sub-agent execution, convergence, and compaction
  are all pluggable interfaces. Future agents implement only what is unique to them.
- **Provider-agnostic.** A loop is bound to one provider (so context, cache, and thinking
  signatures stay coherent), but different agents/sub-agents can run on different providers.
- **Streaming-first.** The provider interface is a single streaming method; non-streaming is
  just "drain the channel."
- **Serializable state.** The conversation `Context{System, Messages, Tools}` is the single
  serializable source of truth — persistence, mid-run model switching, and sub-agent transfer
  fall out of it for free.

## What works today

```go
import (
    "context"
    "fmt"

    "github.com/jelmersnoeck/agentloop"
    "github.com/jelmersnoeck/agentloop/llm"
    "github.com/jelmersnoeck/agentloop/llm/mock"
)

// The mock provider replays scripted event streams, so you can drive and test the
// whole loop with zero network. Real providers (Anthropic, OpenAI) are on the roadmap.
provider := mock.New(mock.TextTurn("hello from the loop"))

// Run blocks, streams every event to the callback, and returns the final result.
res, err := agentloop.Run(context.Background(), "say hi",
    agentloop.WithProvider(provider),
    agentloop.WithModel("mock-model"),
    agentloop.WithOnEvent(func(e llm.Event) {
        if e.Type == llm.EventText {
            fmt.Print(e.Text)
        }
    }),
)
if err != nil {
    panic(err)
}
fmt.Println(res.FinalText)
```

Need a handle for async steering? Construct an `Agent` and call `Run` on it — then
`agent.Steer("...")` / `agent.Follow("...")` from any goroutine inject messages the loop picks
up at its next checkpoint:

```go
agent, _ := agentloop.New(agentloop.WithProvider(provider))
go watchForInput(agent) // calls agent.Steer(...) whenever
res, err := agent.Run(ctx, "start the task", onEvent)
```

The intended zero-config entry point — `agentloop.New(agentloop.WithDefaultConfig())`, which
auto-detects a provider from the environment — arrives with the real providers milestone.

## Architecture

```
agentloop/            // public API: Agent, options, the command/event loop
  llm/                // dependency-free contracts — imported by everything
    mock/             // scriptable provider for offline testing
    anthropic/        // (planned) wraps anthropic-sdk-go
    openai/           // (planned) net/http + SSE
  tool/               // (planned) Tool interface + Registry + builtins
  subagent/           // (planned) observable spawner
  agentgroup/         // (planned) one-shot fan-out/fan-in orchestration
  agentsmd/           // (planned) AGENTS.md / CLAUDE.md discovery
  cache/              // (planned) cross-provider caching policy
```

The load-bearing rule: **`llm` depends on nothing else in the module; every other package
depends only on `llm`.** That single constraint keeps the SDK decoupled and testable.

The full design lives in
[`docs/superpowers/specs/2026-07-19-agentloop-sdk-design.md`](docs/superpowers/specs/2026-07-19-agentloop-sdk-design.md).

## Roadmap

The SDK is built bottom-up as a sequence of independently-shippable milestones:

1. **Foundation** *(done)* — `llm` contracts, mock provider, minimal `Run` loop.
2. **Tool framework** — `tool` interface, registry, `FromFunc` schema generation, `tool/truncate`.
3. **Built-in tools** — `read`, `write`, `edit`, `bash`, `glob`, `grep`.
4. **Full loop** — tool execution, steering/follow-up queues, hooks, convergence.
5. **Real providers** — Anthropic + OpenAI, multi-provider router, logical tiers.
6. **Sub-agents & groups** — observable spawner, fan-out/fan-in orchestration.
7. **Config & caching** — `AGENTS.md` loading, cross-provider caching.

## Development

Requires Go 1.26+.

```bash
go test ./...        # run the suite
go test -race ./...  # the loop and mock are concurrent
go vet ./...
gofmt -l .           # should print nothing
```

Contributor and agent conventions live in [`AGENTS.md`](AGENTS.md).

## Inspiration

Draws on [jelmersnoeck/forge](https://github.com/jelmersnoeck/forge),
[earendil-works/pi](https://github.com/earendil-works/pi) (formerly badlogic/pi-mono), and the
Anthropic, OpenAI, and Gemini SDKs.
