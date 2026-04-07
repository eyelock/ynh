Evaluate all tutorials affected by recent changes. For each affected tutorial:

1. Build and install the latest binaries (`make build && make install`)
2. Run each tutorial step that produces verifiable output
3. Compare actual output against the expected output documented in the tutorial
4. Record every mismatch — do not fix anything during evaluation

At the end, produce a structured report:

- **File**: tutorial path
- **Step**: which T-number
- **Expected**: what the tutorial says
- **Actual**: what was produced
- **Cause**: whether the mismatch is from our changes or pre-existing
- **Severity**: cosmetic (wording/formatting) vs structural (wrong paths/formats)

Do not attempt fixes. The goal is a complete picture of what needs updating before committing.
