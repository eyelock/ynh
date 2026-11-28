package main

// launchClass says whether a command main.go dispatches can start something a harness declares
// (a sensor, a hook, an MCP server, a vendor session): a launching command must reach the one trust
// decision (decideTrust) before it starts anything, and a command that does not launch says why
// it needs none (ynh #1006, H2). A test holds this table to main.go's switch, so a command added
// there without a row here fails the suite, and runs each launching command against an
// unaccepted harness.
type launchClass struct {
	// Launches is true when the command can run a harness's executables.
	Launches bool
	// Why is the reason, for a row that does not launch (or an internal launcher).
	Why string
}

// commandClasses classifies every command main.go dispatches, by its first word (the flag spellings
// --version, --help and -h are help and version).
var commandClasses = map[string]launchClass{
	// Launching: each reaches decideTrust, by way of the command named here.
	"run":          {true, "starts the vendor session, with the harness's hooks and MCP servers"},
	"agent":        {true, "agent run (and --resume) starts a vendor worker and runs the harness's sensors"},
	"check":        {true, "runs the harness's sensors"},
	"sensors":      {true, "sensors run runs a sensor command; ls and show only read"},
	"baseline":     {true, "baseline --explain runs the harness's sensors to resolve the findings it forgives; without --explain it only reads"},
	"image":        {true, "builds an image that carries the harness and its acceptance, both forms"},
	"delegate-run": {true, "a delegation's session is a ynh run of an installed, accepted harness"},

	// Internal launchers: started by a session or a run that has already passed the gate, with
	// arguments the gate vetted, never by a person.
	"mcp-exec":     {false, "internal: starts one MCP server of a harness a run already admitted"},
	"mcp-headers":  {false, "internal: resolves the headers of an MCP server a run already admitted"},
	"delegate-mcp": {false, "internal: the delegate tools' MCP server of an admitted parent; each delegate is resolved and checked when it starts"},
	"parent-mcp":   {false, "internal: the parent's MCP server for a delegate session an admitted run started"},
	"inbox-mcp":    {false, "internal: the inbox MCP server of an admitted run; it reads and writes messages, runs no harness executable"},

	// Not launching: they read or change ynh's own state, or print, and run nothing a harness declares.
	"init":       {false, "creates a harness folder"},
	"install":    {false, "fetches and writes a harness; --trust only records acceptance"},
	"uninstall":  {false, "removes a harness"},
	"remove":     {false, "alias of uninstall"},
	"update":     {false, "fetches sources"},
	"ls":         {false, "lists"},
	"list":       {false, "alias of ls"},
	"info":       {false, "prints a harness"},
	"installed":  {false, "prints install records"},
	"schema":     {false, "prints schemas"},
	"vendors":    {false, "lists vendors"},
	"sources":    {false, "lists sources"},
	"paths":      {false, "prints paths"},
	"status":     {false, "prints status"},
	"ps":         {false, "lists running sessions"},
	"stop":       {false, "stops a session"},
	"reap":       {false, "removes finished session records"},
	"search":     {false, "searches registries"},
	"registry":   {false, "manages registries"},
	"backend":    {false, "manages backends"},
	"delegate":   {false, "edits delegates_to"},
	"fork":       {false, "copies a harness"},
	"include":    {false, "edits includes"},
	"focus":      {false, "lists focuses"},
	"profile":    {false, "lists profiles"},
	"hook":       {false, "lists hooks"},
	"doctor":     {false, "reads and reports"},
	"mcp":        {false, "lists and reports MCP servers"},
	"trust":      {false, "reads and records acceptance"},
	"telemetry":  {false, "reports telemetry"},
	"prune":      {false, "deletes run folders"},
	"migrate":    {false, "migrates the home"},
	"quarantine": {false, "moves a broken install aside"},
	"version":    {false, "prints the version"},
	"help":       {false, "prints help"},
}
