---
name: ynh-team-setup
description: Guide graduation from a personal persona to a team setup with delegation. Creates a team persona that delegates to personal personas.
---

# Team Persona Setup

You are guiding a user through creating a team persona that delegates to personal personas. This is the "graduation" from individual use to team-wide adoption.

## Before you start

Read these references to understand delegation and Git URL formats:

1. Read `references/delegation.md` for `delegates_to` syntax, Git URL formats, auth, and vendor support
2. Read `testdata/team-persona/.claude-plugin/plugin.json` and `testdata/team-persona/metadata.json` for the team persona structure

## Step 1: Understand their current setup

Ask the user:
- Do they already have a personal persona? What's it called?
- Is it in a Git repo already, or just local?
- What does their team look like? (size, shared standards, tooling)

## Step 2: Explain the delegation model

Explain how ynh delegation works (see `references/delegation.md`):

**Delegation** means a team persona knows about personal personas. When someone runs the team persona and asks for something that a personal persona handles, the AI vendor can delegate to it as a subagent.

Two modes of use:
- **Delegation for quick tasks**: Run `team-dev "deploy checklist"` and it delegates to the right specialist
- **Dedicated sessions for deep work**: Run `david` directly when you need your full personal context

The team persona bundles shared standards (rules, skills) while individual personas carry personal preferences.

## Step 3: Team persona location

Ask where to create the team persona. Options:
- A new Git repo (recommended for teams - everyone installs from the URL)
- A local directory (fine for testing first)

Suggest a name like `team-dev` or `<company>-dev`.

## Step 4: Team artifacts

Ask what shared artifacts the team needs:
- **Rules** - Coding standards, review checklist, testing requirements
- **Skills** - Shared workflows (deploy, review, incident response)
- **Agents** - Team specialists (security reviewer, architecture advisor)
- **Commands** - Common operations (CI checks, deploy)

They might also want to pull from external repos via `includes`.

## Step 5: Generate the team persona

Create the team persona directory.

`.claude-plugin/plugin.json`:

```json
{
  "name": "<team-name>",
  "version": "0.1.0",
  "description": "<team description>"
}
```

`metadata.json`:

```json
{
  "ynh": {
    "default_vendor": "<vendor>",
    "includes": [],
    "delegates_to": [
      {"git": "<personal-persona-git-url>"}
    ]
  }
}
```

**Git URL format for delegates_to** - see `references/delegation.md` for the three formats:
- Shorthand: `github.com/user/persona` (expands to SSH)
- Full SSH: `git@github.com:user/persona.git`
- Full HTTPS: `https://github.com/user/persona.git`

If the personal persona isn't in Git yet, explain they'll need to push it first for delegation to work. Show them how:

```bash
cd <persona-dir>
git init && git add . && git commit -m "Initial persona"
# Push to their Git hosting
```

Generate any shared artifacts they requested (rules, skills, etc.) following standard formats (skills need `SKILL.md` with frontmatter, agents need markdown with frontmatter, rules and commands are plain markdown).

## Step 6: Installation for the team

Show the team installation flow:

```bash
# Team member installs the team persona
ynh install <team-persona-git-url>
team-dev                    # interactive session with team config

# They can also install their own personal persona
ynh install <personal-persona-git-url>
david                       # personal session
```

Explain the vendor standardization: setting `default_vendor` in `metadata.json` ensures everyone uses the same AI vendor, but individuals can override with `-v`.

## Step 7: Auth considerations

If any repos are private, explain the auth model (see `references/delegation.md`). Key points:
- SSH URLs (`git@github.com:...`) recommended for private repos
- ynh delegates to the local `git` binary - if `git clone` works, ynh works
- Team members each need their own SSH keys / credentials configured

## Step 8: Next steps

After the team persona is working:

1. **Version with Git tags** - Use `ref: v1.0.0` in includes for stable references
2. **Monorepo support** - If the org has a monorepo with AI config, use the `path` field in `delegates_to` (see `references/delegation.md`).
3. **Multiple teams** - Each team can have their own persona that delegates to specialists. Personas compose infinitely.
