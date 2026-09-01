#!/bin/sh
# Does each command's --format json output still match the schema this
# repository publishes for it?
#
# Twice that pair drifted apart and nothing noticed: search.schema.json
# documented `id` as installable when it was not (#337), and
# test/golden/search.json had repo and path inverted against both the code and
# the description (#342). Neither was caught by a test, because nothing
# compared emitted output to the published contract on a schedule.
#
# Validates against the schema files ON DISK, not the ones embedded in the
# binary. That is deliberate and is what makes this sensor calibratable: an
# embedded-schema check gives the same answer in every directory, so no
# fixture could ever trip it, and a sensor that cannot be tripped cannot be
# proven to still observe anything.
set -u

SCHEMA_DIR="docs/schema/cli"
if [ ! -d "$SCHEMA_DIR" ]; then
	echo "no $SCHEMA_DIR in $(pwd) — this sensor has nothing to observe here"
	exit 1
fi

fail=0
checked=0

# Commands that emit a documented shape with no arguments. Others need a
# harness or an id, so they are left to the tutorials and the golden files.
for pair in "ls:list" "vendors:vendors" "paths:paths" "version:version"; do
	cmd="${pair%%:*}"
	sch="${pair##*:}"
	schema="$SCHEMA_DIR/$sch.schema.json"

	[ -f "$schema" ] || continue
	checked=$((checked + 1))

	if ! out=$(ynh "$cmd" --format json 2>/dev/null) || [ -z "$out" ]; then
		echo "ynh $cmd produced no JSON"
		fail=1
		continue
	fi
	if ! err=$(printf '%s' "$out" | ynd validate-output --schema "$schema" 2>&1 >/dev/null); then
		echo "ynh $cmd no longer matches $sch.schema.json"
		echo "  $err"
		fail=1
	fi
done

# A checker that finds nothing is not a checker that found nothing wrong.
if [ "$checked" -eq 0 ]; then
	echo "no schemas matched in $SCHEMA_DIR — the pairs and the directory have diverged"
	exit 1
fi

if [ "$fail" -eq 0 ]; then
	echo "$checked commands match the schemas this repository publishes"
fi
exit "$fail"
