# ynh-maven-plugin

Maven integration for [ynh](https://github.com/eyelock/ynh) — the AI coding harness manager.

AI capabilities via standard Maven config. `mvn verify` can include an AI security review. `mvn ynh:execute -Dynh.focus=review` for on-demand use.

## Quick Start

```xml
<plugin>
  <groupId>io.ynh</groupId>
  <artifactId>ynh-maven-plugin</artifactId>
  <version>0.1.0</version>
  <configuration>
    <vendor>claude</vendor>
    <inlineConfig>
      <focus>
        <review>
          <profile>ci</profile>
          <prompt>Review staged changes for quality</prompt>
        </review>
      </focus>
    </inlineConfig>
  </configuration>
</plugin>
```

```bash
mvn ynh:execute -Dynh.focus=review
```

## Goals

| Goal | Description | Interactive |
|------|------------|-------------|
| `ynh:run` | Launch interactive AI session | Yes |
| `ynh:execute` | Run with a focus or prompt | No (CI-friendly) |

## Configuration

### Plugin-level

| Parameter | Property | Description | Default |
|-----------|----------|-------------|---------|
| `vendor` | `ynh.vendor` | Vendor name (claude, codex, cursor) | — |
| `inlineConfig` | — | Inline harness config (XML) | — |
| `outputDir` | `ynh.outputDir` | Where to write generated .harness.json | `target/ynh/` |

### Execute goal

| Parameter | Property | Description |
|-----------|----------|-------------|
| `focus` | `ynh.focus` | Named focus entry from config |
| `prompt` | `ynh.prompt` | Direct prompt (alternative to focus) |
| `skip` | `ynh.skip` | Skip execution (for conditional CI) |

### Run goal

| Parameter | Property | Description |
|-----------|----------|-------------|
| `profile` | `ynh.profile` | Named profile to activate |

## Usage Examples

### On-demand review
```bash
mvn ynh:execute -Dynh.focus=review
```

### Automated in CI (bound to verify phase)
```xml
<executions>
  <execution>
    <id>ai-security-review</id>
    <phase>verify</phase>
    <goals><goal>execute</goal></goals>
    <configuration>
      <focus>security</focus>
      <skip>${skipAiReview}</skip>
    </configuration>
  </execution>
</executions>
```

```bash
mvn verify                          # runs AI security review
mvn verify -DskipAiReview=true      # skips it
```

### Interactive session
```bash
mvn ynh:run
mvn ynh:run -Dynh.profile=ci
```

## How It Works

1. Plugin reads `<inlineConfig>` from pom.xml
2. `HarnessWriter` serializes to `.harness.json` in `target/ynh/` (camelCase XML → snake_case JSON, `vendor` → `default_vendor`)
3. `ynh run --harness-file target/ynh/.harness.json` is exec'd

## Binary Resolution

The plugin finds the ynh binary in this order:
1. `-Dynh.binaryPath=/path/to/ynh` (explicit)
2. `ynh` on PATH
3. `~/.ynh/bin/ynh` (Homebrew / `make install`)

## Prerequisites

- Java 17+
- ynh binary installed (via Homebrew or from GitHub Releases)

## Publishing

Maven Central via the Central Publishing Portal. Requires `io.ynh` namespace verification.
