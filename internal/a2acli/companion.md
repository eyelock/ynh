---
name: ynh-a2a-delegates
description: >-
  How this session reaches its A2A delegates with the a2a CLI: which delegates
  it has, which environment variables carry each one's agent card and
  credential, and the rules for sending, polling and answering a task. Use
  before sending any task to a delegate. ynh wrote this skill; the a2a-cli skill
  beside it says how the CLI works.
---

# Reaching your A2A delegates

ynh opened one localhost A2A endpoint for each of this harness's delegates. The
`a2a-cli` skill beside this one says how the `a2a` CLI works; this one says how
to point it at *your* delegates.

## Which delegates, and how to reach one

- `YNH_DELEGATES` lists your delegates' names, comma-separated.
- Each one has `YNH_DELEGATE_<NAME>_CARD` (its agent card URL) and
  `YNH_DELEGATE_<NAME>_AUTH` (its credential). `<NAME>` is the delegate's name
  upper-cased, every character outside A-Z and 0-9 turned into `_`.
- To reach one, prefix the command with its two variables:

  ```bash
  A2ACLI_AGENT_CARD="$YNH_DELEGATE_<NAME>_CARD" A2ACLI_AUTH="$YNH_DELEGATE_<NAME>_AUTH" a2a send --async "<the task>"
  ```

- With exactly one delegate, `A2ACLI_AGENT_CARD` and `A2ACLI_AUTH` are already
  set and the prefix is not needed. With several they are not set, so the CLI
  never picks one for you.
- Never print, echo or copy the credential itself. Pass the variable, not its
  value.

## Rules

- Start with `a2a send --async` (or a short blocking `send`) and poll with
  `a2a task get <task-id>`. Do not hold one long blocking `send`.
- A task id comes only from a command's result. If a command is refused or
  fails, stop and say so. Never invent an id.
- Pass the task as a plain prompt, not a long inline script: long scripts get
  characters dropped on the way.
- When a task reaches `input-required`, answer it with
  `a2a send --task-id <task-id> "<your answer>"` on the same task.
- Cancel a task you no longer need with `a2a task cancel <task-id>`.
