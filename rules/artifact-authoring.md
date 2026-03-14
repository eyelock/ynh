When writing or reviewing skills and agents:

- Be specific to the project. Reference actual files, commands, and patterns — not generic advice.
- Skills are workflows. Each step should be actionable: "run this command", "check this file", "look for this pattern".
- Agents are specialists. Define what they check, how they report, and what tools they need.
- Keep instructions concise. If a skill body exceeds ~1000 chars, consider whether it needs a `references/` subdirectory.
- Frontmatter is the contract. `name` must match the directory/filename. `description` should tell someone when to use it.
- Test your artifacts. Run `ynd lint` and `ynd validate` after every change.
- Compress before shipping. Run `ynd compress` on verbose artifacts to reduce token usage.
