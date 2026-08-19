SSPGPT v0.7.1 MemoryService v2 runtime data

MemoryService creates runtime state here on demand:
- episodes_v2.jsonl: immutable completed interaction journal.
- semantic_v2.jsonl: validated first-class semantic memories.
- raw_recent_v2.jsonl: append-only compact chronological dialogue journal. It is the source for unbounded Replay recall and is not embedded/reranked or used as semantic-memory authority.
- index/: versioned embedding vectors.
- models/: downloaded Qwen GGUF models.
- inference/: pinned llama.cpp runner/runtime and download/cache assets only.

All diagnostic logs are centralized under ghost/master/logs/.
Local inference diagnostics are written to logs/inference/{memory_llm,embedder,reranker}.log.
Legacy memory/inference/*.log files are migrated automatically on MemoryService startup.

The NAR intentionally contains no user memory, vector index, generated runtime logs, or model files.
Models are downloaded according to config/local_models.json and verified against the pinned model SHA-256 values.
