# 0002 v0 uses HTTP + JSON + SSE on localhost

Date: 2026-11-14 · Status: accepted; the transport stayed HTTP + JSON + SSE (see 0031) · Spec: 0.5, 11, 16.1

Status, 23 Nov: ConnectRPC and tsnet never came. The transport stayed HTTP + JSON + SSE; 0031 put every host call under `/api/h/<hostId>/`.

**Context.** The spec decides Protobuf + ConnectRPC over tsnet with device pairing. v0 has one desktop client on the same machine.

**Options.** (a) Build the full M0 transport first. (b) Plain HTTP + JSON commands and a Server-Sent Events stream on 127.0.0.1, with the same message shapes (`command`, snapshot + upserts + events).

**Decision.** (b). It removes code generation, pairing and certificates from the first version and keeps the command path and the stream semantics the spec defines. The server accepts only loopback Host headers and requires a custom request header on commands, which blocks cross-site requests and DNS rebinding.

**Consequences.** M0 replaces the transport. The client talks to one module (`web/src/api.ts`), so the swap touches one file on the client.
