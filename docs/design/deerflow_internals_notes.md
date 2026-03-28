# DeerFlow harness internals (source-level)

Deep read of [bytedance/deer-flow](https://github.com/bytedance/deer-flow) under `backend/packages/harness/deerflow/`. For a shorter comparison to ADK Go, see [deerflow_adk_comparison.md](./deerflow_adk_comparison.md).

## Architecture spine

- **Lead agent** is LangChain `create_agent` (`agents/lead_agent/agent.py`), not a hand-written node graph in that file.
- Reliability is mostly **middleware** + typed **`ThreadState`** (`agents/thread_state.py`): `sandbox`, `thread_data`, `artifacts`, `todos`, `viewed_images`, etc.
- Middleware **order is documented in comments** (e.g. dangling-tool patch before model sees history; summarization early; clarification last).

## P0-style safety layers

### Loop detection (`agents/middlewares/loop_detection_middleware.py`)

- After each model message with `tool_calls`, hashes a **multiset** of `(name, args)` (sorted JSON) and tracks a **per-thread sliding window** (LRU cap on threads).
- After **warn_threshold** identical hashes: inject a **HumanMessage** warning (not SystemMessage—**Anthropic** forbids mid-conversation system messages; see comment referencing issue #1299).
- After **hard_limit**: **strip `tool_calls`** from the last AI message and append a forced-stop string—forcing a **text** turn instead of spinning until recursion limit.

### Dangling tool calls (`agents/middlewares/dangling_tool_call_middleware.py`)

- Uses **`wrap_model_call`**: for each AI message whose `tool_call` ids lack matching `ToolMessage`, inserts synthetic **error** `ToolMessage`s **immediately after** that AI message (not appended at end of history).
- Fixes **invalid transcripts** after cancel/interrupt—weak models cannot recover from protocol errors without this.

### Tool errors (`agents/middlewares/tool_error_handling_middleware.py`)

- **`wrap_tool_call`**: exceptions become **`ToolMessage` with `status="error"`** (detail truncated); **`GraphBubbleUp`** is re-raised so LangGraph interrupts still work.

### Subagent fan-out cap (`agents/middlewares/subagent_limit_middleware.py`)

- **`after_model`**: if more than `max_concurrent` **`task`** tool calls appear in one response, **truncates** the excess. Limit clamped to **[2, 4]**.
- System prompt explicitly says excess calls are **silently discarded**—**dual enforcement** (narrative + code).

## Context / token discipline

- **`SummarizationMiddleware`** with configurable triggers (`messages` / `tokens` / `fraction`), optional cheaper summarizer model, trim of text fed to summarization (`config/summarization_config.py`).
- **`DeferredToolFilterMiddleware`**: deferred MCP tools stay in ToolNode but **schemas are removed from `bind_tools`**; discovery via **`tool_search`**—fewer tools in the model’s context.

## Clarification as control flow

- **`ClarificationMiddleware`** intercepts `ask_clarification` in **`wrap_tool_call`**, returns **`Command(..., goto=END)`**—hard stop until the UI/session resumes.

## Subagent execution (`tools/builtins/task_tool.py`, `subagents/executor.py`)

- Subagent gets its own `create_agent` with **`build_subagent_runtime_middlewares`** (no uploads, no dangling-tool patch—different policy than lead).
- **`task` tool disabled** for subagents to prevent recursion; tools filtered by allow/deny lists.
- **Background execution + server-side polling** in `task_tool` (sleep loop, timeout from `timeout_seconds + 60s`); streams `task_*` events—parent model does not need a reliable polling plan.

## Memory (`agents/middlewares/memory_middleware.py`, `agents/memory/updater.py`)

- **Filters** messages before memory update: drops tool chatter, keeps “final” AI messages; strips `<uploaded_files>` so session paths are not memorized as facts.
- **`MemoryUpdater._extract_text`** handles list-shaped model content (avoids broken `str(list)`); strips markdown fences; **`json.loads`**; regex scrubs upload sentences from persisted summaries/facts.

## Sandbox + guardrails

- **`SandboxMiddleware`**: lazy vs eager acquire; reuse per thread; release in `after_agent` / context.
- **`GuardrailMiddleware`**: pre-tool evaluation; deny → error `ToolMessage`; provider failure → optional **fail-closed**.

## Model resolution (`agents/lead_agent/agent.py`)

- Unknown `model_name` in request → **fallback** to default with warning; thinking disabled if model does not support it.

---

## Honest evaluation (after E2E on our ADK pattern)

We added **runner-level E2E tests** in `examples/reliable_pipeline/pipeline_e2e_test.go` using `internal/testutil.NewTestAgentRunner` and `CollectTextParts`. They assert:

1. **Good simulated model path**: full `runner.Run` → three sub-agents → final text contains `[ok] Parsed intent="echo"` and the user string.
2. **Broken simulated model path**: final text is **`[fallback]`** and still echoes normalized user input (no silent empty failure).
3. **Long input**: normalize cap (4000) + broken model → fallback still contains a long run of characters (truncation + safe echo).

### Does this prove the DeerFlow-style pattern is “worth it”?

**What the tests actually validate**

- The **plumbing** is correct: sequential workflow, **session `State().Set/Get`** with `temp:` keys through a real **Runner**, and **deterministic** post-steps when structured output fails.
- The **repair helper** is doing real work in tests in `repair_test.go` (fenced JSON, chatter).

**What they do not validate**

- No **real LLM**; no **tool-call loops**, **dangling tools**, or **multi-turn** recovery—those are where DeerFlow’s middleware shines.
- No measurement of **latency**, **token cost**, or **user-visible quality** vs “just prompt harder.”

### Self-check questions (answers we can justify from tests + code review)

| Question | Answer |
|----------|--------|
| Does the pipeline still complete when “model” output is garbage? | **Yes** (E2E fallback test). |
| Is anything gained over unit-testing `ParseIntentJSON` alone? | **Yes**: proves **Runner + sequential agents + session state** work together; catches wiring mistakes. |
| Is this comparable to DeerFlow’s loop/dangling-tool middleware? | **No**—those are **additional** layers; our example is a **minimal** structural analogue. |
| When is it worth shipping in production? | When you have **hard downstream requirements** (schema, billing, safety) and cannot trust the model to always emit valid structure—then **validate-or-fallback in Go** pays off. |
| When is it marginal? | When the task is chat-only, single-step, and failures are acceptable—extra stages add **latency and complexity**. |

**Bottom line:** The E2E tests show the **pattern is sound and non-brittle for the simulated failure mode**. They do **not** by themselves prove parity with DeerFlow’s full harness; they justify **keeping** the example as a **template** and **regression guard** for teams that will add a real `llmagent` in the middle and keep the validator.
