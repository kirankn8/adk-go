# DeerFlow vs ADK Go: harness patterns and weak LLMs

This note compares [DeerFlow](https://github.com/bytedance/deer-flow) (ByteDance’s LangGraph/LangChain “super agent” harness) with **ADK Go** (`google.golang.org/adk`), focused on **robustness when the language model is unreliable**.

**Related:** [deerflow_internals_notes.md](./deerflow_internals_notes.md) (middleware, loop detection, memory filtering—source-level). **E2E:** `go test ./examples/reliable_pipeline/...` exercises `runner.Run` + sequential agents + fallback path.

## What DeerFlow optimizes for

- **Explicit orchestration**: A LangGraph-style graph encodes allowed transitions; long-horizon work is decomposed into steps, sub-agents, and tools rather than one free-form chat pass.
- **Environment boundaries**: Sandboxed execution and filesystem/workspace patterns reduce “the model IS the computer” failure modes.
- **Context engineering**: Summarization, progressive skill loading, and offloading to files/memory shrink what must fit in the model’s context.
- **Runtime vs model**: Skills, tools, and gateways implement policy; the model proposes, the harness constrains.

## What ADK Go provides today

- **Agent tree + transfers**: Routing can be model-driven (`llmagent` + descriptions) or structural (`SubAgents`, transfer actions).
- **Workflow agents**: `sequentialagent`, `parallelagent`, and `loopagent` fix **order**, **concurrency**, and **iteration caps** in code—similar in spirit to a graph edge you did not delegate to the LLM.
- **Tools & confirmation**: Function tools and optional human confirmation gates add non-LLM veto points.
- **Session state & artifacts**: Cross-step data and files without stuffing everything into the prompt.

## When the LLM is “dumb as a rock”

| Risk | DeerFlow-style mitigation | ADK Go analogue |
|------|---------------------------|-----------------|
| Wrong or missing structure | Graph + structured outputs + validation in harness | `sequentialagent` + **Go validation** after an LLM step; `output_key` / schema where supported |
| Rambling or fenced JSON | Parsing/repair in tooling | **Repair/extract JSON in Go** (see `examples/reliable_pipeline`) |
| Skipping tools | Tool policies, retries, explicit tool nodes | Narrow tool sets, **callbacks** (`BeforeModel` / `AfterModel`), or replace routing with a **custom `Run`** for critical branches |
| Unbounded loops | Max iterations, termination conditions | `loopagent` **MaxIterations**, escalate actions |
| Unsafe execution | Sandbox | **Tool confirmation**, avoid arbitrary shell tools for weak models; external sandbox if needed |

## Practical takeaway

Neither framework “fixes” a bad model magically. Both stay usable when you **move control to code**: fixed workflows, strict validation, deterministic fallbacks, small tool surfaces, and hard caps. DeerFlow ships more batteries (sandbox, skills layout, IM channels); ADK Go stays closer to **compose agents in Go** and use workflow agents plus session state as your harness.

The `examples/reliable_pipeline` example demonstrates a minimal pattern: **normalize → model text → validate/repair or fallback**. The default middle step is a **simulated** weak model; **`BuildReliablePipelineWithLLM`** in `pipeline_llm.go` swaps in a real **`llmagent`** with `OutputKey` set to the same session key the validator reads, and an `Instruction` template `{temp:reliable_normalized_text}` filled from the normalize step. Runner tests cover simulated paths plus **mock-model E2E** for the LLM-shaped pipeline. See the **Honest evaluation** section in [deerflow_internals_notes.md](./deerflow_internals_notes.md) for what that does and does not prove.
