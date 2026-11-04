package vendor

import "fmt"

// workspaceDeclarations is each vendor's answer to "how do I grant a folder
// beside the project": a flag, or the reason there is none. The values are the
// <x>WorkspaceFlag and <x>WorkspaceUnsupported declarations in each adapter's
// file, which scripts/vendor-parity.sh requires of every vendor.
var workspaceDeclarations = map[string]struct{ flag, unsupported, byDefault string }{
	"claude":   {claudeWorkspaceFlag, claudeWorkspaceUnsupported, ""},
	"codex":    {codexWorkspaceFlag, codexWorkspaceUnsupported, ""},
	"copilot":  {copilotWorkspaceFlag, copilotWorkspaceUnsupported, ""},
	"cursor":   {cursorWorkspaceFlag, cursorWorkspaceUnsupported, ""},
	"opencode": {opencodeWorkspaceFlag, opencodeWorkspaceUnsupported, opencodeWorkspaceDefault},
}

// WorkspaceArgs renders granted folders as the vendor's flags, one pair per
// folder. A vendor that allows folders by default returns no args and no
// reason: they are reachable already. A vendor with no equivalent returns no args and the reason, which
// the caller reports against each folder: a folder is never dropped silently.
func WorkspaceArgs(vendorName string, dirs []string) (args []string, unsupported string, err error) {
	d, ok := workspaceDeclarations[vendorName]
	if !ok {
		return nil, "", fmt.Errorf("%w %q: no workspace declaration", ErrUnknownVendor, vendorName)
	}
	if d.flag == "" {
		return nil, d.unsupported, nil // empty when byDefault: nothing to render, nothing refused
	}
	for _, dir := range dirs {
		args = append(args, d.flag, dir)
	}
	return args, "", nil
}
