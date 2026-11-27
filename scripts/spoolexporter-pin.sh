#!/usr/bin/env bash
# Fail when the spoolexporter library in go.mod is newer than the ynr pin in the
# Dockerfile: a newer library would write a spool the pinned ynr cannot read.
# Compares major, then minor (patch releases never change the spool), so
# spoolexporter v1.0.0 fails against a pin of 0.6.0.
#
#   spoolexporter-pin.sh <ynr-pin> <spoolexporter-version>   compare two versions
#   spoolexporter-pin.sh --self-test                          run the cases
set -euo pipefail

# newer_minor LIB PIN: succeeds when LIB's major.minor is above PIN's.
newer_minor() {
  local lib="${1#v}" pin="${2#v}"
  local lib_mm pin_mm
  lib_mm="$(echo "$lib" | cut -d. -f1,2)"
  pin_mm="$(echo "$pin" | cut -d. -f1,2)"
  [ "$lib_mm" != "$pin_mm" ] &&
    [ "$(printf '%s\n%s\n' "$lib_mm" "$pin_mm" | sort -V | tail -n1)" = "$lib_mm" ]
}

if [ "${1:-}" = "--self-test" ]; then
  fail=0
  check() { # want LIB PIN
    local want="$1" got=ok
    newer_minor "$2" "$3" && got=newer
    if [ "$got" != "$want" ]; then
      echo "FAIL: spoolexporter $2 against pin $3: want $want, got $got" >&2
      fail=1
    fi
  }
  check ok v0.6.0 0.6.0
  check ok v0.6.3 0.6.0
  check ok v0.5.0 0.6.0
  check ok v0.9.0 0.10.0
  check newer v0.7.0 0.6.0
  check newer v0.10.0 0.9.0
  check newer v1.0.0 0.6.0
  check newer v1.0.0 0.99.0
  check ok v1.0.0 1.0.2
  [ "$fail" -eq 0 ] && echo "spoolexporter-pin: all cases pass"
  exit "$fail"
fi

[ "$#" -eq 2 ] && [ -n "$1" ] && [ -n "$2" ] || {
  echo "usage: $0 <ynr-pin> <spoolexporter-version> | --self-test" >&2
  exit 2
}
echo "ynr pin $1, spoolexporter $2"
if newer_minor "$2" "$1"; then
  echo "spoolexporter $2 is newer than the ynr pin $1: move YNR_VERSION in the Dockerfile" >&2
  exit 1
fi
