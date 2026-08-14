CharacterGPT v0.5 stable online-update channel

Baseline: v0.5.23

This channel intentionally distributes only the safe text/configuration layer.

Included:
- descript.txt
- dic00_system.txt
- dic01_chat.txt
- dic02_touch.txt
- satori_bootconf.txt
- config/*

Explicitly excluded:
- bridge/*
- character/*
- profile/*
- satori.dll
- shell/*

Bridge/runtime binary changes require an NAR or a future restart-safe binary updater.
Local character definitions and runtime/memory state are never overwritten by this channel.
