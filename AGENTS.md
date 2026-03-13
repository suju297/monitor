# Mempack Agent Policy

Before starting any task, fetch repo memory:
- Prefer MCP: call `mempack_get_context` with the user's task as the query.

If MCP is unavailable, ask the user to run:
`mem get "<task>" --format prompt`

Full instructions: `.mempack/MEMORY.md`
