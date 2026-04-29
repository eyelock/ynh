# Tutorial 19: Sensors

Declare observation surfaces that a loop driver — CI, an orchestrator, a custom tool — runs between agent turns. ynh declares; the loop driver runs.

This tutorial walks through every sensor source variant, the validation rules, and the discovery surface a loop driver consumes. It does **not** demonstrate a working loop driver — that is out of scope for ynh. See [Sensors reference](../sensors.md) for the full schema and consumer guide.

## Prerequisites

```bash
rm -rf /tmp/ynh-tutorial
ynh uninstall sensor-demo 2>/dev/null

mkdir -p /tmp/ynh-tutorial/sensor-harness/.ynh-plugin
```

## T19.1: A `files` sensor

The simplest sensor reads pre-existing artifacts. Create a harness that declares a coverage sensor:

```bash
cat > /tmp/ynh-tutorial/sensor-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "sensors": {
    "coverage": {
      "category": "maintainability",
      "source": { "files": ["coverage/lcov.info"] },
      "output": { "format": "lcov-summary" }
    }
  }
}
EOF

ynd validate /tmp/ynh-tutorial/sensor-harness
```

Expected output: `valid`.

## T19.2: A `command` sensor

Add a sensor that runs a shell command. Edit the manifest:

```bash
cat > /tmp/ynh-tutorial/sensor-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "sensors": {
    "build": {
      "category": "maintainability",
      "source": { "command": "make check" },
      "output": { "format": "text" }
    },
    "coverage": {
      "category": "maintainability",
      "source": { "files": ["coverage/lcov.info"] },
      "output": { "format": "lcov-summary" }
    }
  }
}
EOF

ynd validate /tmp/ynh-tutorial/sensor-harness
```

## T19.3: A `focus`-referencing sensor

Sensors can reuse a top-level focus. Add a focus and reference it:

```bash
cat > /tmp/ynh-tutorial/sensor-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focus": {
    "audit-vulns": {
      "prompt": "Identify any high-severity vulnerabilities in the diff vs main."
    }
  },
  "sensors": {
    "build": {
      "source": { "command": "make check" },
      "output": { "format": "text" }
    },
    "security": {
      "category": "behaviour",
      "source": { "focus": "audit-vulns" },
      "output": { "format": "markdown" }
    }
  }
}
EOF

ynd validate /tmp/ynh-tutorial/sensor-harness
```

## T19.4: An inline-focus sensor

Sometimes a focus exists only to drive one sensor. Inline it directly:

```bash
cat > /tmp/ynh-tutorial/sensor-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focus": {
    "audit-vulns": {
      "prompt": "Identify any high-severity vulnerabilities in the diff vs main."
    }
  },
  "sensors": {
    "build": {
      "source": { "command": "make check" },
      "output": { "format": "text" }
    },
    "security": {
      "source": { "focus": "audit-vulns" },
      "output": { "format": "markdown" }
    },
    "coverage-judge": {
      "role": "convergence-verifier",
      "source": {
        "focus": {
          "prompt": "Assess if test coverage is adequate for the changed surface area."
        }
      },
      "output": { "format": "markdown" }
    }
  }
}
EOF

ynd validate /tmp/ynh-tutorial/sensor-harness
```

Inline focuses are scoped to the sensor that declares them. Install the harness and confirm the inline focus does **not** appear in the top-level Focus list:

```bash
ynh install /tmp/ynh-tutorial/sensor-harness
ynh info sensor-demo | grep -A 3 'Focus:'
```

You should see only `audit-vulns` listed under Focus — `coverage-judge` is a sensor, and its inline focus is hidden inside the sensor declaration.

## T19.5: Discovery — `ynh sensors ls/show`

Loop drivers discover what's declared via the CLI:

```bash
ynh sensors ls sensor-demo
```

Expected (trimmed):

```
NAME              CATEGORY          SOURCE     FORMAT
build             -                 command    text
coverage-judge    -                 focus*     markdown
security          -                 focus      markdown

* = inline focus
```

The JSON form is what a loop driver actually consumes:

```bash
ynh sensors ls sensor-demo --format json
```

To resolve a single sensor with its focus expanded into a self-contained payload:

```bash
ynh sensors show sensor-demo security
```

The string focus reference (`"focus": "audit-vulns"`) is expanded inline so the consumer doesn't have to look up the focus declaration separately.

## T19.6: Validation — every rule produces a clear error

Demonstrate one validation rule. Set two source fields:

```bash
cat > /tmp/ynh-tutorial/sensor-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-demo",
  "version": "0.1.0",
  "sensors": {
    "broken": {
      "source": { "command": "x", "files": ["a"] },
      "output": { "format": "text" }
    }
  }
}
EOF

ynd validate /tmp/ynh-tutorial/sensor-harness
```

Expected error (the schema-level violation and the cross-field violation each emit one line):

```
sensors/broken/source: 'oneOf' failed, subschemas 0, 1 matched
sensor "broken": source must have exactly one of files, command, focus
```

## T19.7: Hook–sensor pairing

The most common production pattern: a hook produces an artifact, a sensor declares its contract over that artifact. Re-link the harness to the previous sensors plus a hook:

```bash
cat > /tmp/ynh-tutorial/sensor-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "after_tool": [
      { "matcher": "Edit|Write", "command": "./scripts/run-lint.sh > .lint-results.json" }
    ]
  },
  "sensors": {
    "lint": {
      "category": "maintainability",
      "source": { "files": [".lint-results.json"] },
      "output": { "format": "json" }
    }
  }
}
EOF

ynd validate /tmp/ynh-tutorial/sensor-harness
```

The hook produces the file mid-session; the sensor consumes it between iterations. Coupling is by shared file path — implicit, no schema link needed.

## T19.8: Install round-trip preserves sensors

Re-install the harness and confirm sensors still appear in the discovery surface:

```bash
ynh install /tmp/ynh-tutorial/sensor-harness
ynh sensors ls sensor-demo --format json | jq '.[] | .name'
```

Expected: each sensor name appears, one per line. Sensors flow from the source `plugin.json` through the install path into the on-disk harness directory and surface back through the discovery CLI unchanged.

Note: `ynd export` produces vendor-native plugin formats (Claude/Cursor/Codex), and vendors have no concept of sensors today. Sensors live only in the ynh manifest and are consumed by loop drivers via `ynh sensors`. They are not part of vendor export.

## T19.9: Cleanup

```bash
ynh uninstall sensor-demo
rm -rf /tmp/ynh-tutorial
```

## What a loop driver does with this

A loop driver wraps an agent runtime (Claude Code, Codex, etc.) and runs sensors between turns. Discovery is `ynh sensors ls --format json`; resolution is `ynh sensors show --format json`; execution is `ynh sensors run`. ynh emits raw signal — exit codes, output, file contents — and the loop driver turns that into pass/fail policy and feedback for the next turn.

ynh ships no loop driver. See [Sensors reference §"Consuming sensors"](../sensors.md#consuming-sensors-for-loop-driver-authors) for the consumer pattern, and [harness engineering](../harness-engineering.md) for the architectural framing.

## What you learned

- A sensor is a role declaration over existing primitives — no new artifact type.
- Three source variants: `files`, `command`, `focus` (string ref or inline).
- Inline focuses are scoped to the sensor that declares them.
- The discovery surface (`ls`, `show`) is the contract loop drivers consume.
- Hooks and sensors are complementary — push vs pull, in-session vs between-turn.

Hooks often pair with sensors — see [Tutorial 4 (Hooks)](10-hooks.md).
