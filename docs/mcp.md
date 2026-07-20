# HTTP MCP onboarding

`flotilla mcp` registers an HTTP Model Context Protocol server in a supported
harness and hands OAuth back to the human. It never accepts or stores provider
tokens.

## Higgsfield on Claude Code

Register the public Higgsfield endpoint in Claude Code's user configuration:

```bash
flotilla mcp add --harness claude --transport http --scope user \
  higgsfield https://mcp.higgsfield.ai/mcp
```

Then the human operator completes browser OAuth from an interactive terminal:

```bash
flotilla mcp login --harness claude higgsfield
```

The login command delegates to the harness-owned OAuth flow. Do not paste a
token into chat, Discord, a model prompt, or `flotilla-secrets.env`.

Claude user configuration follows the active `CLAUDE_CONFIG_DIR`. Run both
commands in the same Claude account/config context used by the target fleet
seat. With no `CLAUDE_CONFIG_DIR`, Claude's default user configuration is used.

You can inspect the native registration afterward without exposing OAuth
material:

```bash
claude mcp get higgsfield
```

## Higgsfield on Codex

Codex uses its user configuration for HTTP MCP servers:

```bash
flotilla mcp add --harness codex higgsfield https://mcp.higgsfield.ai/mcp
flotilla mcp login --harness codex higgsfield
```

Claude is the default harness, so `--harness claude` may be omitted. OpenCode
and other harnesses remain unsupported until they have a non-interactive,
testable registration contract.

## Boundaries

- The first release supports HTTP MCP endpoints only. Remote endpoints must use
  HTTPS; plaintext HTTP is accepted only on loopback. URL credentials and query
  parameters are rejected so authentication stays in the harness OAuth flow.
- Claude scopes are `user`, `local`, and `project`; the default is `user`.
  Codex registration is user-scoped.
- Registration and OAuth do not authorize paid image generation. Higgsfield
  paid or metered use still requires the fleet's normal money approval.
- OAuth credentials stay in the selected harness's own credential store. They
  are not copied into flotilla state or shared fleet secrets.

Provider reference: <https://higgsfield.ai/cli>
