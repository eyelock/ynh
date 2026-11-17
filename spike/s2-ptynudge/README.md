Spike for ynh #908 (S2). Not for merge. A unix-only pty wrapper that types a nudge into an idle vendor TUI.
Run: s2-ptynudge -nudge-file F [-idle 2s -quiet 3s -hold-dirty=true -paste] -- vendor-cmd
Windows is not covered: creack/pty has no Windows support (ConPTY needed).
