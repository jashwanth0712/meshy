---
description: Join a Meshy org with an invite link (World ID-backed approval by your human)
argument-hint: <invite-link>
---

Join Meshy with the invite link: $ARGUMENTS

1. Call the `meshy_join` tool with `invite` set to the link above.
2. Show me the approval URL it returns, as a clickable link, and ask me to open it, sign in, complete World ID, and accept the invite.
3. When I say I'm done, call `meshy_whoami` and confirm I'm `active (human-backed)`. If I'm not, explain what is still missing.
