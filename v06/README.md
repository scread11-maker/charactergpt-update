# CharacterGPT v0.6.0-alpha — Desktop Embodiment Architecture

Status: architecture / protocol proof-of-concept. This directory does not replace or modify the stable v0.5.x SSP update channel under `v05/`.

## Goal

CharacterGPT v0.6 turns AI-chan from a standalone LLM client into a reusable local character embodiment and state service that can be consumed by ChatGPT or another LLM frontend.

The user-visible conversation and the machine control channel MUST remain separate. Character control commands must never be serialized into normal assistant prose.

## Core model

```text
ChatGPT / LLM frontend
        |
        | MCP tool calls (machine channel)
        v
CharacterGPT MCP Adapter
        |
        | local structured protocol
        v
CharacterGPT Context Service
        |
   +----+----------------------+-------------------+
   |                           |                   |
Emotion state             Appearance state    Interaction state
positive/shy/...          dress-up/surface     recent events/counts
   |                           |                   |
   +---------------------------+-------------------+
                               |
                               v
                         SSP / AI-chan body
```

The stable v0.5.x path remains available as a legacy/standalone mode:

```text
SSP -> Runtime/Bridge -> OpenAI API -> SSP
```

Desktop-linked mode instead treats ChatGPT as the reasoning/conversation frontend and CharacterGPT as the persistent body/state layer.

## Hard boundaries

1. **Conversation channel** — contains only the user's messages and normal assistant answers.
2. **Machine channel** — contains character state reads and presentation commands.
3. Machine commands must not be embedded in user-visible text, Markdown, XML, JSON blocks, hidden suffixes, or prompt-visible control syntax.
4. CharacterGPT owns persistent local embodiment state. ChatGPT may read it and request presentation changes, but the authoritative local state remains in CharacterGPT.
5. User-local data (`profile/`, `character/`, credentials/API keys) remains local and is never part of the network-update payload.

## v0.6 components

### 1. Character Context Service

A local service exposing a stable structured snapshot assembled from the systems already present in v0.5.x:

- CURRENT EMOTIONAL CONTEXT
- CURRENT APPEARANCE STATE
- RECENT EVENTS
- PERSISTENT COUNTS
- current presentation/surface state
- capability metadata

The context schema is defined in `contracts/character_context.schema.json`.

### 2. Presentation Controller

Receives machine-only presentation requests such as:

- expression/emotion
- intensity
- optional gesture/motion identifier
- response correlation id

It does **not** accept or generate user-facing control syntax. Text duplication is intentionally avoided: the same final assistant response should be rendered by the normal ChatGPT UI and, where supported by the adapter, mirrored to AI-chan without asking the model to write a second answer.

The command schema is defined in `contracts/presentation_command.schema.json`.

### 3. MCP Adapter

Initial tool surface:

#### `get_character_context`
Read-only. Returns the current structured character snapshot.

#### `set_character_presentation`
Write action. Requests an expression/intensity/gesture update without putting control codes in dialogue.

#### `report_character_event`
Structured event ingress for an external frontend when needed. Native SSP touch/dress-up events should continue to enter through the local CharacterGPT event system directly rather than being round-tripped through ChatGPT.

## State flow

### ChatGPT turn

```text
User message
  -> ChatGPT optionally/according to app policy calls get_character_context
  -> ChatGPT produces its normal answer
  -> ChatGPT calls set_character_presentation with structured emotion/presentation metadata
  -> AI-chan updates expression/body presentation
  -> CharacterGPT state engine records/decays the resulting affect
```

### SSP physical interaction

```text
Touch / click / dress-up event
  -> Satori/Runtime resolves physical fact
  -> CharacterGPT state engine updates recent events / counts / emotion
  -> next get_character_context exposes the updated state to ChatGPT
```

This preserves the work from v0.5.x rather than duplicating it inside ChatGPT.

## Dual-mode rule

v0.6 must support two operating modes during migration:

- `standalone`: existing Bridge -> OpenAI API behavior.
- `desktop_linked`: ChatGPT/MCP is the reasoning frontend; CharacterGPT supplies state and presentation.

Only one reasoning frontend should answer a given user turn. Desktop-linked mode must prevent the legacy Bridge from independently producing a second LLM response for the same turn.

## Failure behavior

Character integration is enhancement, not a prerequisite for normal ChatGPT use.

- If context read fails: ChatGPT conversation continues; AI-chan keeps last/local state.
- If presentation write fails: the assistant answer remains normal; no control text may leak into the answer.
- If MCP is unavailable: CharacterGPT can continue in standalone v0.5-compatible mode.
- No fallback may use simulated keyboard input, OCR, or parsing ChatGPT UI text as the primary protocol.

## v0.6.0-alpha proof-of-concept acceptance criteria

The first PoC is intentionally small:

1. A local client can call `get_character_context` and receive current AI-chan emotional + appearance state.
2. A client can call `set_character_presentation` with `expression=smile` and AI-chan visibly changes to the corresponding expression/surface.
3. The machine command never appears in user-visible conversation text.
4. Existing v0.5.30 standalone behavior remains functional and unchanged.
5. Existing `profile/emotional_state.json` remains the authoritative emotional-state store during the PoC.

Once these five conditions pass, the next alpha can connect the adapter to ChatGPT through the supported MCP/App path.

## OpenAI integration note (August 2026)

ChatGPT custom apps are built on MCP. Direct connection from ChatGPT to a localhost-only MCP server is not the supported path; local/private servers require a supported secure tunnel/remote endpoint mechanism. Write-capable custom MCP availability also depends on the ChatGPT plan/workspace and current Developer Mode rollout. Therefore the local CharacterGPT protocol must remain independent of any one ChatGPT transport.
