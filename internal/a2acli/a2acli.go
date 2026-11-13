// Package a2acli is the shell route to a harness's A2A delegates: the official
// a2a CLI (github.com/a2aproject/a2a-cli) and its skill. ynh vendors the skill
// unchanged at a pinned commit, writes it with a short companion skill of its
// own into the parent's vendor skill folder when a run has A2A delegates, and
// lets doctor check the binary, the skill and the vendor settings the route
// needs (docs/delegate-sessions.md, "The a2a CLI route").
package a2acli

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The pin. Updating the vendored skill is a deliberate change: replace
// vendored/a2a-cli/SKILL.md, vendored/a2a-cli/UPSTREAM and these together.
const (
	// CLIVersion is the a2a CLI release the route is built and tested against.
	CLIVersion = "0.4.0"
	// SkillCommit is the commit of a2aproject/a2a-cli the skill was vendored from.
	SkillCommit = "5c95123d1c8d50a1cd6dfc2f28a14f5533a383ae"
	// SkillVersion is the skill's own metadata version at that commit.
	SkillVersion = "2026.09.22"
)

// Names of the two skills written into the vendor's skill folder.
const (
	SkillName     = "a2a-cli"
	CompanionName = "ynh-a2a-delegates"
)

//go:embed vendored/a2a-cli/SKILL.md vendored/a2a-cli/LICENSE vendored/a2a-cli/UPSTREAM companion.md
var files embed.FS

func mustRead(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		panic(err) // embedded at build time
	}
	return b
}

// Skill is the vendored a2a-cli skill file, exactly as upstream has it.
func Skill() []byte { return mustRead("vendored/a2a-cli/SKILL.md") }

// License is the upstream licence, kept beside the vendored skill.
func License() []byte { return mustRead("vendored/a2a-cli/LICENSE") }

// Companion is ynh's own skill about reaching this parent's delegates.
func Companion() []byte { return mustRead("companion.md") }

// WriteSkills writes the two skills under skillsDir, the vendor's skill folder
// (<run dir>/<config dir>/skills).
func WriteSkills(skillsDir string) error {
	set := []struct {
		dir, name string
		data      []byte
	}{
		{SkillName, "SKILL.md", Skill()},
		{SkillName, "LICENSE", License()},
		{CompanionName, "SKILL.md", Companion()},
	}
	for _, f := range set {
		dir := filepath.Join(skillsDir, f.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("writing the %s skill: %w", f.dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o644); err != nil {
			return fmt.Errorf("writing the %s skill: %w", f.dir, err)
		}
	}
	return nil
}

// SkillState is what a vendor's skill folder holds of the vendored skill.
type SkillState int

const (
	SkillMissing SkillState = iota
	SkillDiffers
	SkillCurrent
)

// CheckSkill compares the a2a-cli skill in skillsDir with the pin.
func CheckSkill(skillsDir string) SkillState {
	got, err := os.ReadFile(filepath.Join(skillsDir, SkillName, "SKILL.md"))
	if err != nil {
		return SkillMissing
	}
	if !bytes.Equal(got, Skill()) {
		return SkillDiffers
	}
	return SkillCurrent
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// ParseVersion pulls the first major.minor.patch out of `a2a version` output.
func ParseVersion(out string) (string, bool) {
	v := versionRe.FindString(out)
	return v, v != ""
}

// Older reports whether version a is older than b, both major.minor.patch.
func Older(a, b string) bool {
	pa, pb := split(a), split(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func split(v string) [3]int {
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(s)
	}
	return out
}

// Binary reports the a2a CLI on PATH: whether it is there and what version it
// says. A binary that does not report a version is found with version "".
func Binary() (found bool, version string) {
	path, err := exec.LookPath("a2a")
	if err != nil {
		return false, ""
	}
	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return true, ""
	}
	v, _ := ParseVersion(string(out))
	return true, v
}
