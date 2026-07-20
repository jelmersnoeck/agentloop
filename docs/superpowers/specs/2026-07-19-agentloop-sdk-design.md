# agentloop SDK — Design

**Date:** 2026-07-19
**Status:** Approved for planning (user waived the review gate)
**Module:** `github.com/jelmersnoeck/agentloop`
**Language:** Go 1.26+

---

## 1. Purpose & Goals

`agentloop` is a reusable Go SDK for building agents. It provides the loop that takes
instructions, talks to a pluggable LLM provider to make decisions, and executes what the
model asks for — plus the surrounding machinery (tools, sub-agents, agent groups, AGENTS.md
config, caching) that every agent otherwise re-implements.

The explicit goal is to be a **great base for future agents**, so the design optimizes for:

- **Extensibility** — providers, tools, sub-agent execution, convergence, and compaction are
  all pluggable interfaces. Future agents implement only what is unique to them.
- **Provider-agnosticism** — the SDK is *not* shaped by any one vendor's SDK. Different
  agents/sub-agents can run on different providers; a loop can switch model per message.
- **Performance** — streaming-first, concurrent tool execution, prompt-cache-aware assembly.
- **Ergonomics** — `agentloop.New(agentloop.WithDefaultConfig())` works out of the box;
  everything is overridable via composable functional options.

### Inspirations

- **jelmersnoeck/forge** (Go) — the core loop shape, concurrent read-only tool execution,
  scoped tool registries for sub-agents, prompt-cache determinism, AGENTS.md hierarchy.
- **earendil-works/pi** (formerly badlogic/pi-mono, TS) — the serializable
  `Context{system, messages, tools}`, steering vs follow-up queues, first-class thinking
  content, reasoning tiers, and the caching *retention* abstraction.
- **Anthropic SDK / Claude Agent SDK** — the tool-runner loop shape, `jsonschema` struct-tag
  tools (the Go idiom), thinking-signature replay, prompt-cache breakpoint semantics,
  server-side context editing.
- **OpenAI / Gemini SDKs** — automatic prefix caching and explicit `CachedContent`, which
  shaped the provider-agnostic caching abstraction.

### Non-goals (v1)

- No CLI or product binary — this is an SDK only.
- No built-in permission/approval UI — sandboxing is the consumer's concern (pi's stance).
- No user-defined sub-agent *presets* from AGENTS.md frontmatter (deferred; see §5).
- No Gemini provider implementation in v1 (the router is built to take it as the first
  drop-in adapter afterward).

---

## 2. Package Layout

```
agentloop/            // public API: Agent, options, the run loop, command/event types
  llm/                // dependency-free contracts — imported by EVERY other package
    anthropic/        // Provider impl (wraps anthropic-sdk-go)
    openai/           // Provider impl (net/http + SSE, no SDK dependency)
  tool/               // Tool interface, Registry, FromFunc reflection helper
    builtin/          // read, write, edit, bash, glob, grep
    truncate/         // direction-aware output caps + temp-file spillover
  subagent/           // Spawner, Spec, Handle, the `agent` tool
  agentgroup/         // one-shot Group orchestration (WaitAll/First/N, reducers)
  agentsmd/           // AGENTS.md / CLAUDE.md discovery + system-prompt assembly
  cache/              // Policy, Retention, CacheUsage
  convergence/        // ConvergencePolicy + default anti-runaway strategy
```

**The load-bearing rule:** `llm` depends on nothing else in the module; every other package
depends only on `llm`, never on each other's internals. This is the single decision that
keeps the SDK decoupled and testable. (forge's only reuse blocker was that its equivalent
`types` package sat in `internal/`; ours is public.)

---

## 3. The `llm` Contracts Package

The serializable heart. `Context` is the single source of truth for a conversation;
persistence, mid-run model switching, and sub-agent transfer all fall out of it being
serializable.

```go
// Context is the serializable state of a conversation.
type Context struct {
    System   []Block      // system prompt + injected reminders/AGENTS.md instructions
    Messages []Message
    Tools    []ToolSchema
}

type Role string // "user" | "assistant"

type Message struct {
    Role    Role
    Content []Block
}
```

### Blocks — a typed interface union

Blocks are a Go interface with concrete types (pi's discriminated-union model, chosen over
forge's flat tagged struct for type-safety and because thinking must stay distinct). JSON
round-tripping uses a registered type discriminator with ~20 lines of custom marshaling glue.

```go
type Block interface{ blockType() BlockType }

type TextBlock       struct{ Text string }
type ThinkingBlock   struct{ Text string; Signature string } // signature is MANDATORY to replay
type ToolUseBlock    struct{ ID, Name string; Input json.RawMessage }
type ToolResultBlock struct{ ToolUseID string; Content []Block; IsError bool }
```

> **Thinking-signature requirement (Anthropic):** when extended thinking is used *with tools*,
> the prior `thinking` block — including its opaque `signature` — MUST be replayed in the next
> request or the turn is rejected. The loop preserves `ThinkingBlock`s in history verbatim;
> it never strips them. This is why thinking is a typed block, not a string.

### Provider — one streaming method

```go
// Streaming is the ONLY mode. Non-streaming callers drain the channel.
type Provider interface {
    Stream(ctx context.Context, req Request) (<-chan Event, error)
}

type Request struct {
    Model     string
    Reasoning Reasoning   // neutral tier; the adapter maps it (see below)
    Context   Context
    MaxTokens int
    Cache     cache.Policy // see §9
}

// Event is the normalized streaming delta.
type Event struct {
    Type     EventType   // text_delta | thinking_delta | tool_use_start | tool_use_delta |
                         // tool_use_stop | usage | message_stop | error
    Text     string
    ToolCall *ToolCallDelta
    Usage    *Usage
    Err      error
}

type Usage struct {
    InputTokens, OutputTokens int
    Cache CacheUsage // §9
}
```

### Reasoning tiers

A neutral enum the **adapter** maps per-provider (the complexity stays inside the adapter):

```go
type Reasoning string // "minimal" | "low" | "medium" | "high" | "xhigh"
```

- **Anthropic** maps to `thinking:{type:"adaptive", effort:...}` on newer models, or
  `{type:"enabled", budget_tokens:N}` on older ones — a per-model mapping table in the adapter.
- **OpenAI** maps to `reasoning_effort`.

### Optional capability interfaces (type-asserted at runtime)

A minimal provider implements only `Stream`. Extra capabilities are separate interfaces so
providers opt in without bloating the core contract:

```go
type ModelDefaulter interface{ DefaultModel() string }
type ModelMatcher   interface{ Serves(model string) bool }         // for the Router, §8
type ModelLister    interface{ Models(ctx context.Context) ([]ModelInfo, error) }
```

---

## 4. Entry Point & Configuration

Functional options. `WithDefaultConfig` is a *composition* of ordinary options, not a special
constructor — "figure it out for me" and "precise control" share one code path.

```go
agent, err := agentloop.New(agentloop.WithDefaultConfig())

agent, err := agentloop.New(
    agentloop.WithProvider(anthropic.New(anthKey)),   // additive; call repeatedly
    agentloop.WithProvider(openai.New(oaiKey)),
    agentloop.WithTier("smart", llm.ModelRef{Model: "claude-opus-4-8", Reasoning: llm.ReasoningHigh}),
    agentloop.WithTier("fast",  llm.ModelRef{Model: "claude-haiku-4-5", Reasoning: llm.ReasoningLow}),
    agentloop.WithModel("smart"),
    agentloop.WithTools(tool.Defaults()...),
    agentloop.WithAgentsMD(),
    agentloop.WithCache(cache.Policy{Retention: cache.Short}),
    agentloop.WithConvergence(convergence.Default()),
    agentloop.WithHooks(myHooks),
)
```

**Provider auto-detection** (`WithDefaultConfig` expands to this) resolves in order and
**registers every provider it finds a key for**:

1. `AGENTLOOP_PROVIDER` env var
2. `~/.agentloop/config` default
3. `ANTHROPIC_API_KEY` present → register Anthropic
4. `OPENAI_API_KEY` present → register OpenAI
5. none found → return a **clear error** ("set a key or pass WithProvider"), never a nil provider

Defaults bundled by `WithDefaultConfig`: `tool.Defaults()`, `WithAgentsMD()`,
`cache.Short`, `convergence.Default()`.

---

## 5. The Run Loop

`Run` is a **blocking call that streams events to a callback**, the idiomatic Go shape for a
continuous loop (cf. `filepath.WalkDir`, `bufio.Scanner`). Output flows out through the
`EventFunc`; asynchronous input (steering, follow-ups) flows in through thread-safe methods.
This separation keeps the common case a one-liner while still supporting mid-run injection.

```go
// Output: a blocking Run that streams events and returns the final result.
type EventFunc func(llm.Event)
type Result   struct{ FinalText string; Messages []llm.Message }

func (a *Agent) Run(ctx context.Context, task string, onEvent EventFunc) (Result, error)

// Async input: thread-safe, callable from any goroutine, drained at checkpoints.
func (a *Agent) Steer(text string)  // high-priority, injected ahead of follow-ups
func (a *Agent) Follow(text string) // sequential follow-up

// One-shot package-level convenience (onEvent supplied via WithOnEvent):
func Run(ctx context.Context, task string, opts ...Option) (Result, error)
```

`Run` ends when the assistant produces no tool calls and no messages remain queued, or when
`ctx` is cancelled — there is no `Stop` sentinel and no channel to close. Sub-agent events
bubble up by the parent forwarding them onto the parent's `onEvent` (see §7).

### One iteration

```
Run(ctx, task, onEvent):
  append task as a user message
  for {
    if ctx cancelled → return (result, ctx.Err())

    // CONVERGENCE CHECK
    if policy.ShouldConverge(state) → inject directive / finish

    // ASSEMBLE + CALL
    ctx.Transform()                          // pluggable compaction hook (default no-op)
    req  := assemble(system, history, tools, model, reasoning, cache)   // cache-friendly, §9
    ch   := retry(provider.Stream(ctx, req))
    msg  := collectAssistantMessage(ch, onEvent)  // forwards every Event delta to onEvent

    history = append(history, msg)

    // EXECUTE TOOLS (if any)
    calls := msg.ToolUseBlocks()
    if len(calls) > 0 {
        for each call:
            hooks.BeforeToolCall → execute → hooks.AfterToolCall
            // read-only tools fan out concurrently; mutating tools run sequentially
            // AFTER each call: mini-drain steering queue   ← fine-grained steering
        history = append(history, toolResults)    // as a user message
        continue
    }

    // TURN BOUNDARY: no tool calls → run hooks, then drain queued input
    d := hooks.AfterTurn(ctx, &msg)          // Continue | Inject(text) | Finish
    if d.Finish → return (result, nil)
    if next := drain(steeringQueue, then followUpQueue); next != "" {
        append next as a user message; continue
    }
    return (result, nil)                      // converged: no tools, nothing queued
  }
```

### Steering vs follow-up queues

`Steer()` and `Follow()` enqueue onto two internal queues, drained at two checkpoints = the two
steering granularities:

- **steering queue** drained **after every tool call** (fine) *and* at the turn boundary
  (coarse). For "no, not that file" mid-run injection.
- **follow-up queue** drained at the turn boundary, steering first. `QueueMode` =
  `AllAtOnce` | `OneAtATime`; **default `OneAtATime`** (each follow-up gets a fresh turn).

Because the methods are thread-safe, a UI or another goroutine calls `agent.Steer(...)` at any
moment; the loop picks it up at the next safe point. The common (no-steering) path never
touches them.

### Hooks / interceptors

The synchronous, programmatic complement to the async queues. Same decision surface.

```go
type Action int // Continue | Inject | Finish

type Decision struct { Action Action; Steer string }

type Hooks struct {
    BeforeToolCall func(ctx context.Context, c *ToolCall) (Decision, error)
    AfterToolCall  func(ctx context.Context, c *ToolCall, r tool.Result) Decision
    AfterTurn      func(ctx context.Context, m *llm.Message) Decision
}
```

**Hook errors are resilient:** a `BeforeToolCall` error surfaces as a `tool_result` error the
model can react to — it does *not* abort the turn.

### Convergence

Pluggable, checked at the turn boundary. Default is anti-runaway; tier-escalation and
multi-agent consensus are future strategies.

```go
type ConvergencePolicy interface {
    // Return (true, directive) to force the model to wrap up, or (true, "") to hard-stop.
    ShouldConverge(s State) (converge bool, directive string)
}
// State: turn count, cumulative token usage, recent tool-call fingerprints, elapsed time.
// convergence.Default() = MaxTurns(50) + a token-budget guard.
```

### Cancellation

Cancellation is the caller's `context.Context`: cancel it and `Run` returns the partial
`Result` (the transcript so far) plus `ctx.Err()`. There is no separate `Stop` — the context
*is* the stop signal, and `Run` returning is the completion signal. The transcript is never
lost to a cancel.

---

## 6. Tools

```go
type Tool interface {
    Name() string
    Description() string
    Schema() Schema
    Execute(ctx context.Context, input json.RawMessage, tctx ToolContext) (Result, error)
}

type ToolContext struct {
    CWD       string
    SessionID string
    Emit      func(llm.Event)   // tools stream their own progress onto the event bus
    Spawn     subagent.Spawner  // sub-agent capability handed to tools (§7)
}

type Result struct {
    Content []llm.Block
    IsError bool
}

// Optional; absent = treated as mutating (the safe default).
type ReadOnly interface{ ReadOnly() bool }
```

### Ergonomic authoring — `tool.FromFunc`

Reflection over a typed args struct generates the schema and unmarshals input — mirroring the
Anthropic Go SDK's `jsonschema` struct-tag idiom. The raw `Tool` interface stays open for
hand-rolled schemas (forge's escape hatch).

```go
type readArgs struct {
    Path   string `json:"path"   jsonschema:"required,description=File to read"`
    Offset int    `json:"offset" jsonschema:"description=Start line"`
}
readTool := tool.FromFunc("read", "Read a file",
    func(ctx context.Context, a readArgs, tctx ToolContext) (Result, error) { ... })
```

### Registry

```go
type Registry struct { /* mutex-guarded map[string]Tool */ }
func (r *Registry) Register(t Tool)
func (r *Registry) Schemas() []llm.ToolSchema                 // NAME-SORTED → cache-stable
func (r *Registry) Execute(ctx, call ToolCall, tctx) Result
func (r *Registry) Filtered(allow, deny []string) *Registry   // scoping for sub-agents
```

### Concurrency

The executor partitions a turn's tool calls by `ReadOnly`: read-only calls **fan out
concurrently** (goroutines + result channel to avoid races), mutating calls run
**sequentially**, all cancellable via `ctx`. This is also what makes batch sub-agent spawning
"wait for all" work for free (§7).

### Built-in tools (`tool/builtin`)

| Tool | Read-only | Notes |
|---|---|---|
| `read`  | ✓ | text + images |
| `write` |   | atomic write |
| `edit`  |   | exact-match replacement |
| `bash`  |   | stdout+stderr, cancellable process group |
| `glob`  | ✓ | file discovery |
| `grep`  | ✓ | content search |

### Output truncation (`tool/truncate`)

Direction-aware caps applied by each tool at its own boundary (pi's verified model, superior
to forge's blunt central head+tail):

- **bash → tail** (final results + errors matter): keep the last N.
- **read → head** (top of file) with `offset`/`limit` paging.
- **grep → per-line** cap at ~500 chars.
- Limits: **2000 lines OR ~50KB**, whichever hits first.
- **Overflow spills to a temp file**, and the truncation marker returned to the model includes
  the path (`[Showing X of Y. Full output: <path>]`) so it can re-read/page — turning a hard
  limit into a recoverable one.

Truncation is a tool-layer concern; it is the up-front guarantee that no single call blows the
window. Conversation-level compaction (§5 `Transform`) is a separate, later concern.

---

## 7. Sub-agents

First-class and **observable** — the synthesis of forge (has them) and pi's objection (a hidden
sub-agent tool = zero visibility). Spawning is a plain capability; sub-agent events **bubble up**
onto the parent's event stream.

```go
type Spawner interface {
    Spawn(ctx context.Context, spec Spec) (Handle, error)
}

type Spec struct {
    Prompt    string        // becomes the sub-agent's first user message
    Tools     []string      // allow-list; empty = inherit parent's (scoped)
    Deny      []string      // subtractive
    Provider  llm.Provider  // OPTIONAL override → different vendor per sub-agent (§8)
    Model     string        // or a tier name
    Reasoning llm.Reasoning
    MaxTurns  int
}

type Handle struct {
    ID     string
    Events <-chan llm.Event  // sub-agent's own stream; parent forwards these up, tagged by ID
    Done   <-chan Result     // final blocks when it converges
}
```

**Mechanism:** `Spawn` builds a **scoped registry** via `parent.Filtered(allow, deny)` (defense
in depth — the sub-agent literally cannot call ungranted tools), constructs a **fresh loop** with
a serialized copy of the shared `Context` and the `Prompt` as first message, runs it in a
**goroutine**, tags and forwards its events onto the parent bus, and returns the result over
`Done`. `context` cancellation propagates.

**One primitive, three consumption shapes** (there is no forced sync/async default):

```go
h, _   := agent.Spawn(ctx, spec)          // async primitive: live events + cancellation
res, _ := agent.SpawnAndWait(ctx, spec)   // delegate-and-block convenience
rs, _  := agent.SpawnGroup(ctx, specs)    // fan-out / fan-in (see §7b)
```

The **model-facing `agent` tool** (opt-in via `WithTools(subagent.Tool())`) is marked
**parallel-safe**, so when the model emits several spawn calls in one turn they run concurrently
and all results return together before the next turn — the Claude-Code `Task`-tool pattern,
achieved via the read-only concurrency of §6.

### 7b. Agent groups (`agentgroup`)

One-shot orchestration layered purely on `Spawner` (no loop internals). This is net-new value
over every reference — neither forge nor the Agent SDK exposes programmatic group coordination.

```go
type Group struct { /* ... */ }

func NewGroup(s Spawner, opts ...GroupOption) *Group
// GroupOption: WithConcurrency(n)  // bounded worker pool
//              WithErrorPolicy(FailFast | CollectAll)
//              WithTimeout(d)

func (g *Group) Add(specs ...Spec)
func (g *Group) Events() <-chan MemberEvent            // merged, tagged by member ID
func (g *Group) Cancel()
func (g *Group) WaitAll(ctx)      ([]Result, error)    // fan-in
func (g *Group) WaitFirst(ctx)    (Result, error)      // race; cancels the rest
func (g *Group) WaitN(ctx, n int) ([]Result, error)    // quorum; cancels stragglers

// Reducer helper: converge N attempts into one (e.g. a judge/vote fn).
func Reduce(results []Result, fn func([]Result) Result) Result
```

v1 groups are **one-shot** (created, run, collected); interfaces stay clean enough to add
long-lived addressable pools later without breaking changes.

---

## 8. Multi-provider routing & tiers

**Rule: provider is bound per-loop; model varies per-message; provider diversity happens at
the agent boundary.**

The loop never talks to "a provider" — it talks to a **`Router`** that resolves each request to
a concrete provider **by model**. The router itself implements `Provider`, so routing is
transparent and the loop stays ignorant of multi-vendor.

```go
type Router struct { /* ... */ }
func (r *Router) Register(p llm.Provider)                                  // provider self-declares served models
func (r *Router) Stream(ctx, req llm.Request) (<-chan llm.Event, error)    // routes by req.Model
```

- **Within a loop:** the provider is **fixed** (resolved at construction from the default
  model). `Request.Model` may change per turn (tiers within one vendor). Because the provider
  never changes mid-conversation, **thinking signatures, prompt cache, and message shape are
  never disturbed**.
- **Across agents:** a sub-agent `Spec` may bind a **different provider**. Cross-provider
  `Context` translation (e.g. Anthropic thinking blocks → `<thinking>` tags for OpenAI) happens
  **once, at the spawn boundary** — a lossy conversion that is fine when seeding a fresh
  sub-agent, never mid-stream.

**Model → provider resolution:** pattern-match where each adapter declares `Serves("claude-*")`,
with an explicit `Spec.Provider` field as the override escape hatch.

**Logical tiers** let agents reference roles, not vendor model IDs:

```go
agentloop.WithTier("smart", llm.ModelRef{Model: "claude-opus-4-8", Reasoning: llm.ReasoningHigh})
agentloop.WithTier("fast",  llm.ModelRef{Model: "gpt-4.1-mini",    Reasoning: llm.ReasoningMinimal})
```

Within a loop, a tier maps to a same-provider model. Across agents, a `Spec` tier/model may
route to any registered provider. Swapping the "fast" tier to a different vendor is a one-line
config change that moves every sub-agent referencing it.

> **Cache tradeoff (surfaced, not prevented):** switching model per-message is allowed but busts
> the Anthropic prompt cache (per-model namespace; reasoning-effort changes also invalidate).
> The cost shows up in `CacheUsage` (§9) so the consumer sees and decides — freedom with a
> visible price tag.

**Validating use case — product-design agent:** Phase 1, one loop (Anthropic) interviews to
determine needed questions and gathers answers. Phase 2, `SpawnGroup` fans the same brief to
sub-agents on Anthropic + OpenAI + Gemini, `WaitAll`, then a reducer synthesizes the competing
analyses. Exercises the loop, tiers, groups, boundary translation, and multi-provider end to end.

**v1 providers:** Anthropic (wraps `anthropic-sdk-go`) + OpenAI (hand-rolled `net/http` + SSE,
no OpenAI SDK dependency). Router built for N; Gemini is the first drop-in adapter afterward.

---

## 9. Cross-provider caching

The three vendors differ on the surface (Anthropic = explicit breakpoints max 4; OpenAI =
automatic prefix caching; Gemini = implicit + explicit `CachedContent`) but share **one
concept: a stable prefix boundary**. The loop controls assembly, so placement is automatic and
internal — **consumers never touch a breakpoint.**

### Consumer surface — one intent-level knob

```go
agentloop.WithCache(cache.Policy{
    Retention: cache.Short,   // Short(~5m) | Long(~1h) | None — a REQUEST; adapters clamp to legal values
    Key:       "session-abc", // optional: prefix-affinity routing / explicit-cache grouping
})
// WithDefaultConfig() → cache.Short.
```

### The loop enforces cache-friendly assembly (helps every provider, even OpenAI's auto path)

- Fixed prefix order: **system → tools → static project context (AGENTS.md) → conversation**.
- **Determinism pass** (the thing pi lacks, forge does): tools **sorted by name**, **canonical
  JSON** schema/arg serialization, **volatile content (date, live status) isolated to a trailing
  dynamic block** so it is never inside the cached prefix.
- **Rolling tail breakpoint:** each turn the stable prefix stays put and the fresh boundary
  advances to the latest exchange; turns 1..N-1 read from cache, only turn N is new.

### Adapter translation (capability pattern; neutral layer stays clean)

- **Anthropic:** auto-place `cache_control` at **system, last tool, rolling tail** (3 of 4;
  last-tool marker gated behind a capability flag — it breaks some Anthropic-compatible
  backends). `Long` → `ephemeral_1hr` where supported.
- **OpenAI:** automatic prefix caching ≥1024 tokens; the policy becomes **layout enforcement +
  `prompt_cache_key` derived from `Key`**. (GPT-5.6+ explicit breakpoints via the Responses API
  are a later opt-in; the seam is ready.)
- **Gemini (future):** implicit + static-first ordering by default; `Long` + large prefix can be
  promoted to an explicit `CachedContent` handle.

### Unified cache reporting (from day one)

```go
type CacheUsage struct {
    ReadTokens    int // anthropic cache_read / openai cached_tokens / gemini cached_content_token_count
    WriteTokens   int // anthropic cache_creation / openai GPT-5.6+ cache_write_tokens; 0 elsewhere
    WriteTokens1h int // anthropic 1h-retention split
}
```

`WriteTokens` is load-bearing: cache writes now cost ~1.25× on both Anthropic and OpenAI
GPT-5.6+, so caching is no longer unconditionally profitable and cost-aware agents need it
exposed.

### Optional advanced strategy (Anthropic-only)

Anthropic's 2026 **server-side context editing** (`clear_tool_uses`) drops old tool results
server-side **while keeping the prefix warm** — strictly better than client-side truncation for
long-running agents. Wired as one selectable strategy in the §5 compaction hook; default off,
capability-gated to Anthropic.

---

## 10. Cross-cutting concerns

### Cancellation
`context.Context` threads through the loop, tools, and sub-agents. `Stop` cancels gracefully and
returns partial results.

### Retry
Provider `Stream` calls are wrapped in a retry policy (`retry.Default()` = exponential backoff on
transient/5xx/stream errors). Stream errors are classified: compact-and-retry (context overflow)
vs plain retry vs bail.

### Observability
A single `llm.Event` channel is the uniform contract — the loop, tools, and sub-agents all emit
onto it. Sub-agent events are tagged by ID and bubbled to the parent. This is the one seam a UI,
logger, or test consumes.

### Testing strategy
A `llm/mock` provider scripts `Event` streams (including tool calls, thinking, usage) so the
**entire loop, tool execution, steering, convergence, and sub-agent orchestration are testable
without a network**. This is a first-class deliverable, not an afterthought — every package ships
with tests driven by the mock provider.

---

## 11. v1 Scope

**In:**
- `llm` contracts (Context, blocks incl. thinking+signature, Provider, Event, Reasoning, Usage).
- Anthropic + OpenAI providers.
- The blocking `Run` loop (callback output, `Steer`/`Follow` methods) with steering + follow-up
  queues, hooks, convergence.
- `tool` (interface, `FromFunc`, registry, concurrency) + 6 built-ins + direction-aware truncation.
- `subagent` (observable spawner, scoped registries, `agent` tool) + `agentgroup` (one-shot).
- `agentsmd` instruction loading (AGENTS.md → CLAUDE.md fallback, hierarchy, static/dynamic split).
- Multi-provider `Router` + logical tiers.
- `cache` policy + determinism pass + unified `CacheUsage`.
- `llm/mock` provider + tests across all packages.

**Deferred:**
- Gemini provider (first drop-in after v1).
- User-defined sub-agent presets from AGENTS.md frontmatter.
- Long-lived / addressable agent groups.
- Tier-escalation and multi-agent-consensus convergence strategies.
- Anthropic server-side context editing (optional, wired but off).
- OpenAI GPT-5.6+ explicit cache breakpoints via Responses API.
- Session persistence / branching (Context is serializable, so this is additive later).

---

## 12. Risks & Open Questions

- **Reasoning-tier mapping drift:** provider effort/thinking APIs are evolving (Anthropic
  adaptive vs budget_tokens; OpenAI reasoning_effort). Isolated in adapters; needs periodic
  maintenance of the mapping table.
- **Anthropic last-tool cache marker:** known to break some Anthropic-compatible backends;
  gated behind a capability flag, default conservative.
- **Cache constants are vendor-volatile:** min-cacheable tokens, discounts, TTLs, and Gemini
  thresholds differ across model versions and disagreed across sources during research. Read
  from per-model config, do not hardcode as cross-cutting constants.
- **Block JSON marshaling:** the typed-interface union needs correct discriminator round-tripping
  (esp. nested `ToolResultBlock.Content`); covered by explicit serialization tests.
- **Cross-provider thinking translation** at spawn boundaries is inherently lossy; acceptable by
  design (fresh sub-agent), but documented so consumers understand mixed-provider handoffs.

---

## 13. References

- forge — `github.com/jelmersnoeck/forge` (`internal/runtime/loop`, `internal/tools`,
  `internal/runtime/provider`, `internal/runtime/task`, `internal/runtime/context`).
- pi — `github.com/earendil-works/pi` (`packages/ai`, `packages/agent-core`,
  `packages/coding-agent`).
- Anthropic: Tool Runner, extended thinking, prompt caching, context editing docs;
  `anthropic-sdk-go`.
- OpenAI prompt caching (Chat Completions + Responses, GPT-5.6+ explicit breakpoints).
- Google Gemini context caching (implicit + `CachedContent`).
