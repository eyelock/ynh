package main

import (
	"strings"
	"sync"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/registry"
	"github.com/eyelock/ynh/internal/resolver"
)

// updateProbe is the test seam for upstream lookups. Production code routes
// through resolver.LsRemote and a registry walk; tests swap probeGit and
// probeRegistry to avoid network I/O.
type updateProbe struct {
	git      func(gitURL, ref string) (sha string, ok bool)
	registry func(name, source, path string) (version, sha string, ok bool)
}

// defaultProbe wires the production probe functions.
//
// git probe: a failed ls-remote returns ok=false so the caller omits the
// _available field — failures degrade to the "unknown" three-state, never
// to an error.
//
// registry probe: walks all configured registries (hitting network via
// resolver.EnsureRepo inside registry.FetchAll) and looks for an entry
// whose Repo+Path match the install. Lookup is best-effort: missing
// registries, lookup misses, and parse failures all collapse to ok=false.
func defaultProbe() updateProbe {
	return updateProbe{
		git: func(gitURL, ref string) (string, bool) {
			sha, err := resolver.LsRemote(gitURL, ref)
			if err != nil || sha == "" {
				return "", false
			}
			return sha, true
		},
		registry: func(name, source, path string) (string, string, bool) {
			cfg, err := config.Load()
			if err != nil || len(cfg.Registries) == 0 {
				return "", "", false
			}
			regs, err := registry.FetchAll(cfg.Registries)
			if err != nil {
				return "", "", false
			}
			for _, reg := range regs {
				for _, entry := range reg.Entries {
					if !registryEntryMatches(entry, name, source, path) {
						continue
					}
					if entry.Version == "" {
						return "", "", false
					}
					return entry.Version, "", true
				}
			}
			return "", "", false
		},
	}
}

// registryEntryMatches identifies the upstream registry entry for an
// installed harness. The install records `source` (the upstream URL) and
// `path` (subdir within the repo); the registry entry's `Repo` and `Path`
// must match. Name fallback handles older installs without a recorded
// source.
func registryEntryMatches(entry registry.Entry, name, source, path string) bool {
	if source != "" {
		entrySource := strings.TrimSuffix(entry.Repo, ".git")
		installSource := strings.TrimSuffix(source, ".git")
		if entrySource == installSource && entry.Path == path {
			return true
		}
	}
	return entry.Name == name && source == ""
}

// fillUpdates populates version_available / ref_available across one or
// more harness entries by running probes concurrently. Bounded fan-out
// keeps a 50-include harness from spawning 50 simultaneous ls-remotes.
//
// Per-probe failure is silent — the corresponding _available field stays
// omitted ("unknown" state). The command itself never errors on probe
// failure; --check-updates is best-effort by contract.
func fillUpdates(entries []listEntry, probe updateProbe) {
	const concurrency = 8

	type job struct {
		harnessIdx int
		includeIdx int // -1 means harness-level
	}

	var jobs []job
	for hi, e := range entries {
		if shouldProbeHarness(e) {
			jobs = append(jobs, job{harnessIdx: hi, includeIdx: -1})
		}
		for ii, inc := range e.Includes {
			if inc.Git != "" {
				jobs = append(jobs, job{harnessIdx: hi, includeIdx: ii})
			}
		}
	}

	if len(jobs) == 0 {
		return
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex // guards entries during write-back

	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()

			if j.includeIdx == -1 {
				probeHarness(&entries[j.harnessIdx], probe, &mu)
				return
			}
			probeInclude(&entries[j.harnessIdx], j.includeIdx, probe, &mu)
		}(j)
	}
	wg.Wait()
}

// shouldProbeHarness reports whether the harness has enough provenance to
// look up upstream. Pure local installs (no installed_from, or only a
// filesystem source) cannot be probed.
func shouldProbeHarness(e listEntry) bool {
	if e.InstalledFrom == nil {
		return false
	}
	switch e.InstalledFrom.SourceType {
	case "registry", "git":
		return e.InstalledFrom.Source != ""
	}
	return false
}

func probeHarness(entry *listEntry, probe updateProbe, mu *sync.Mutex) {
	prov := entry.InstalledFrom
	switch prov.SourceType {
	case "registry":
		version, sha, ok := probe.registry(entry.Name, prov.Source, prov.Path)
		if !ok {
			return
		}
		mu.Lock()
		if version != "" {
			entry.VersionAvailable = version
		}
		if sha != "" {
			entry.RefAvailable = sha
		}
		mu.Unlock()
	case "git":
		// A git-installed harness was tracked at a specific ref; "available"
		// is the current SHA of the same ref upstream.
		ref := entry.RefInstalled
		if harness.IsPinnedRef(ref) {
			// Pinned to a SHA — probe HEAD to surface "is there anything
			// newer on the default branch" rather than re-resolving the SHA.
			ref = ""
		}
		sha, ok := probe.git(prov.Source, ref)
		if !ok {
			return
		}
		mu.Lock()
		entry.RefAvailable = sha
		mu.Unlock()
	}
}

func probeInclude(entry *listEntry, idx int, probe updateProbe, mu *sync.Mutex) {
	inc := entry.Includes[idx]
	ref := inc.RefInstalled
	if harness.IsPinnedRef(ref) {
		ref = ""
	}
	sha, ok := probe.git(inc.Git, ref)
	if !ok {
		return
	}
	mu.Lock()
	entry.Includes[idx].RefAvailable = sha
	mu.Unlock()
}
