CharacterGPT v0.5 stable online-update channel

Baseline: v0.5.24 system presentation hotfix

This channel distributes only the safe text/configuration layer.

Included:
- descript.txt
- dic00_system.txt
- dic01_chat.txt
- dic02_touch.txt
- satori_bootconf.txt
- satori_conf.txt
- config/*

System balloon behavior:
- Normal CharacterGPT system notifications use the same configured dialogue duration.
- 10/15/20/30/60 seconds are mirrored in Satori and Runtime.
- "No automatic close" maps to balloon timeout 0.
- Background dress-up synchronization remains silent.
- Failure messages remain open so the error can be read.

Explicitly excluded:
- bridge/*
- character/*
- profile/*
- satori.dll
- shell/*
