package harness

import (
	"os"
	"path/filepath"
	"strings"
)

// Many community skill repositories keep each skill as a folder at the top
// level, with its SKILL.md at the folder's root and no skills/ directory
// (<repo>/bash-scripting/SKILL.md). An include picks one by naming the folder
// alone, `"pick": ["bash-scripting"]`, and ynh takes it as the skill
// skills/bash-scripting.
//
// A pick has always had the form type/name, and a pick with no type was an
// error, so this changes nothing an include resolved to before: it gives a
// meaning only to a pick that used to fail.

// TopLevelSkill reports whether pick names a top-level skill folder of the
// include at basePath: a single path segment, with no type prefix, naming a
// folder whose SKILL.md is at its root.
func TopLevelSkill(basePath, pick string) bool {
	p := filepath.ToSlash(filepath.Clean(pick))
	if p == "." || strings.Contains(p, "/") || !filepath.IsLocal(p) {
		return false
	}
	info, err := os.Stat(filepath.Join(basePath, p, "SKILL.md"))
	return err == nil && info.Mode().IsRegular()
}

// PickArtifactType is the artifact type a pick copies into: "skills" for a
// top-level skill folder, otherwise the pick's leading type segment.
func PickArtifactType(basePath, pick string) string {
	if TopLevelSkill(basePath, pick) {
		return "skills"
	}
	return strings.SplitN(filepath.ToSlash(pick), "/", 2)[0]
}

// PickedSkillName is the skill a pick brings, and whether it brings one:
// "skills/<name>" or a top-level skill folder "<name>".
func PickedSkillName(basePath, pick string) (string, bool) {
	if TopLevelSkill(basePath, pick) {
		return filepath.ToSlash(filepath.Clean(pick)), true
	}
	if name, ok := strings.CutPrefix(filepath.ToSlash(filepath.Clean(pick)), "skills/"); ok && name != "" && !strings.Contains(name, "/") {
		return name, true
	}
	return "", false
}
