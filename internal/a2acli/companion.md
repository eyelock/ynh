---
name: ynh-a2a-delegates
description: >-
  How this session reaches its A2A delegates with the a2a CLI: which delegates
  it has, which agent card address reaches each one, and the rules for
  sending, polling and answering a task. Use when you send a task to a delegate with the a2a CLI. A live session may also
  have the same delegates as MCP tools (delegate_to_<name>); this skill covers
  the CLI. ynh wrote this skill; the a2a-cli skill beside it says how the CLI
  works.
---

# Reaching your A2A delegates

ynh opened one localhost A2A endpoint for each of this harness's delegates. The
`a2a-cli` skill beside this one says how the `a2a` CLI works; this one says how
to point it at *your* delegates.

## The same delegates as MCP tools

In a live session ynh may also have given you the same delegates as MCP tools,
from the `ynh-delegates` server: `delegate_to_<name>` to start one, and
`delegate_list`, `delegate_status`, `delegate_result`, `delegate_cancel` and
`delegate_reply`. Both ways reach the same delegates; use either. If you start
a task with the tools, follow them with the tools: their `task_id` is not the
task id the CLI prints, and the rules below are the CLI's.

{{REACH}}
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
