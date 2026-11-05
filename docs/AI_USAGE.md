# AI usage disclosure

As ETHGlobal and Arkiv require, this file records where and how AI tools were used.

- **Planning and research:** Claude Code (Claude Opus 5.5) researched the sponsor docs (ENSv2, World, Arkiv, Whitechain, EF), the NetBird codebase and the sandboxing tools, and drafted the build plan. That plan is committed verbatim at [`docs/PLAN.md`](PLAN.md).
- **Code, Lane C (`meshy/`, `agent/claude-plugin/`, `e2e/run-local.sh`, `deploy/Dockerfile.test`):** written with Claude Code (Claude Opus 5.5), in pair-programming sessions with @jashwanth0712. Every component has tests that run on macOS and in a Linux container. The full flow was exercised against real ENSv2 contracts on an anvil Sepolia fork. Bugs that the tests and live runs caught, and how they were fixed, are listed in the commit messages (for example: Arkiv tuple encoding, ENSv2 `resolve()`-only reads, Docker procfs masking).
- **Code, other lanes:** _to be filled in by each lane._
