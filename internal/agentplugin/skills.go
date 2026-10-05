package agentplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Skill is one discovered skill: the immediate child of skills/ that holds
// it, and the name and description its SKILL.md frontmatter declares.
type Skill struct {
	Dir         string // e.g. "skills/deploy", relative to the plugin root
	Name        string
	Description string
}

// validSkillName is the Agent Skills name rule: 1-64 characters, lowercase
// letters, digits and hyphens, no leading, trailing or doubled hyphen
// (https://agentskills.io/specification). It must also equal the directory
// name, which the caller checks.
func validSkillName(name string) bool {
	if name == "" || len(name) > 64 || strings.Contains(name, "--") ||
		strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		lower := r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'
		if !lower && !digit && r != '-' {
			return false
		}
	}
	return true
}

// DiscoverSkills applies §7.1: each immediate child of skills/ with a path
// named exactly SKILL.md that resolves to a regular file inside the plugin
// root is a skill, and nothing deeper is searched. A child that is not a
// conforming skill is skipped and reported; a skills/ that exists but is not
// a directory invalidates the component type (§6.2), which is reported the
// same way with no skills returned.
//
// Absent skills/ is valid absence: nil, nil.
func DiscoverSkills(dir string) ([]Skill, []Diagnostic) {
	root := filepath.Join(dir, SkillsDir)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []Diagnostic{{SkillsDir, err.Error()}}
	}
	if !info.IsDir() {
		return nil, []Diagnostic{{SkillsDir, "exists but is not a directory; skills component disabled"}}
	}
	if err := within(dir, root); err != nil {
		return nil, []Diagnostic{{SkillsDir, err.Error() + "; skills component disabled"}}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []Diagnostic{{SkillsDir, err.Error()}}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var skills []Skill
	var diags []Diagnostic
	for _, e := range entries {
		rel := SkillsDir + "/" + e.Name()
		childInfo, err := os.Stat(filepath.Join(root, e.Name()))
		if err != nil || !childInfo.IsDir() {
			continue // a file directly under skills/ is not a skill and not an error
		}
		md := filepath.Join(root, e.Name(), SkillFile)
		mdInfo, err := os.Stat(md)
		if err != nil {
			if os.IsNotExist(err) {
				diags = append(diags, Diagnostic{rel, "skipped: no SKILL.md"})
			} else {
				diags = append(diags, Diagnostic{rel, "skipped: " + err.Error()})
			}
			continue
		}
		if !mdInfo.Mode().IsRegular() {
			diags = append(diags, Diagnostic{rel, "skipped: SKILL.md is not a regular file"})
			continue
		}
		if err := within(dir, md); err != nil {
			diags = append(diags, Diagnostic{rel, "skipped: SKILL.md " + err.Error()})
			continue
		}
		data, err := os.ReadFile(md)
		if err != nil {
			diags = append(diags, Diagnostic{rel, "skipped: " + err.Error()})
			continue
		}
		s, msgs := parseSkill(e.Name(), string(data))
		if len(msgs) > 0 {
			diags = append(diags, Diagnostic{rel + "/" + SkillFile, "skipped: " + joinIssues(msgs)})
			continue
		}
		s.Dir = rel
		skills = append(skills, s)
	}
	return skills, diags
}

// parseSkill checks the frontmatter rules of the Agent Skills specification
// that make a skill valid: a name that matches its directory and the naming
// rule, and a non-empty description of at most 1024 characters.
func parseSkill(dirName, content string) (Skill, []string) {
	fm := frontmatter(content)
	if fm == nil {
		return Skill{}, []string{"missing YAML frontmatter"}
	}
	var msgs []string
	name := fm["name"]
	switch {
	case name == "":
		msgs = append(msgs, "frontmatter missing required field name")
	case !validSkillName(name):
		msgs = append(msgs, fmt.Sprintf("name %q must be 1-64 lowercase letters, digits and single hyphens", name))
	case name != dirName:
		msgs = append(msgs, fmt.Sprintf("name %q does not match directory %q", name, dirName))
	}
	desc := fm["description"]
	switch {
	case desc == "":
		msgs = append(msgs, "frontmatter missing required field description")
	case utf8.RuneCountInString(desc) > 1024:
		msgs = append(msgs, "description exceeds 1024 characters")
	}
	return Skill{Name: name, Description: desc}, msgs
}

// frontmatter extracts top-level scalar keys from a leading YAML block. It
// reads what the two required fields need and no more: a full YAML parser
// is a dependency this repository has chosen not to carry, and a skill whose
// name or description is not a plain scalar is not one this can validate
// either way.
func frontmatter(content string) map[string]string {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return nil
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		if rest == "---" || strings.HasPrefix(rest, "---\n") {
			end = 0
		} else {
			return nil
		}
	}
	fm := map[string]string{}
	for _, line := range strings.Split(rest[:end], "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		fm[key] = val
	}
	return fm
}
