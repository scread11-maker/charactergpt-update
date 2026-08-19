CharacterGPTLink / Secure MCP Tunnel
===================================

SSPGPT v0.7.1 fix15 uses one linked-interface executable:

  CharacterGPTLink.exe

It embeds:
- the official MCP Go SDK v1.7.0;
- the OpenAI tunnel-client Go SDK v0.0.11;
- the nine SSPGPT linked_chat tools.

The MCP data path is in-memory inside CharacterGPTLink.exe. It does not expose a public web server, does not use Cloudflare Quick Tunnel, and does not require a separate ContextService, McpAdapter, TunnelSetup, or tunnel-client process.

Setup
-----
1. Create/provision a Secure MCP Tunnel in OpenAI and note its tunnel id.
2. Put only the tunnel id in Plug/link_config.json, or set CONTROL_PLANE_TUNNEL_ID in the environment.
3. Provide a runtime tunnel key through the environment variable named by runtime_api_key_env (default: CONTROL_PLANE_API_KEY). The runtime key should have only the tunnel permissions needed to read/use the configured tunnel. Do not paste an API key into link_config.json.
4. In SSPGPT, open CharacterGPT > ChatGPT連動 > 開啟連動. CharacterGPTLink.exe starts only when explicitly enabled; it is not launched during Ghost boot.
5. Configure the corresponding MCP connection/app in ChatGPT to use the Secure MCP Tunnel.

Local control endpoints
-----------------------
CharacterGPTLink.exe binds only a loopback status/control endpoint at 127.0.0.1:8781:
- GET /health
- GET /status
- POST /shutdown

No MCP endpoint is exposed on localhost. MCP messages travel through the embedded in-memory transport between the OpenAI tunnel SDK and the official MCP server.

Security / ownership
--------------------
- Runtime remains authoritative for NOW, linked lifecycle, affect, and EpisodeCommitV2.
- Bridge remains Secondary Brain during linked turns.
- MemoryService remains historical continuity and never overrides current physical truth.
- CharacterGPTLink does not store the runtime API key on disk.
- Hidden reasoning / chain-of-thought is never part of the MCP tool surface.
