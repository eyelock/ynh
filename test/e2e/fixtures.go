//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureTag names the commit of a fixtureRepo that holds only the minimal
// harness. Tag-pinned includes resolve through it.
const fixtureTag = "e2e-fixtures-v1"

// fixtureRepo is a git repository of harness fixtures, built in the test's
// temp dir and served from a bare repository over file://.
//
// It replaces a clone of eyelock/assistants:e2e-fixtures/, which made every
// test that used it depend on GitHub being reachable (#531). The layout is
// the same, so harnesses still live at e2e-fixtures/<name>.
type fixtureRepo struct {
	// URL is the file:// URL of the bare repository. Includes and delegates
	// point at it, and git installs fetch from it.
	URL string
	// Clone is a working clone checked out at SHA. Tests install harnesses
	// from it by path, and ynh's editors write into it.
	Clone string
	// SHA is the commit holding every fixture: the head of main when the
	// repository is built.
	SHA string
	// TagSHA is the commit fixtureTag names, which holds only
	// e2e-fixtures/minimal.
	TagSHA string
}

// newFixtureRepo builds the fixtures in two commits, so that a pinned SHA,
// a tag and a floating ref each resolve to something different:
//
//  1. e2e-fixtures/minimal, tagged fixtureTag.
//  2. fork-source, included-skill and three harnesses that include from the
//     repository itself: floating, pinned to commit 1 by SHA, and pinned to
//     it by tag.
func newFixtureRepo(t *testing.T) fixtureRepo {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	bare := filepath.Join(root, "assistants.git")
	repo := fixtureRepo{URL: "file://" + bare, Clone: filepath.Join(root, "clone")}

	initRepo(t, src)
	writeFixture(t, src, "minimal/.agents/harness/plugin.json", harnessManifest("minimal",
		"Bare-minimum installable harness used as an E2E fixture for ynh.", ""))
	mustGit(t, src, "add", "-A")
	mustGit(t, src, "commit", "--quiet", "-m", "minimal fixture")
	mustGit(t, src, "tag", fixtureTag)
	repo.TagSHA = gitOutput(t, src, "rev-parse", "HEAD")

	writeFixture(t, src, "fork-source/.agents/harness/plugin.json", harnessManifest("fork-source",
		"E2E fixture: a harness designed to be forked.", ""))
	writeFixture(t, src, "included-skill/SKILL.md",
		"---\nname: included-skill\ndescription: A no-op skill used as an include target.\n---\n\n# included-skill\n")
	writeFixture(t, src, "with-floating-include/.agents/harness/plugin.json", harnessManifest("with-floating-include",
		"E2E fixture: a floating-ref include.",
		fmt.Sprintf(`{"git": %q, "path": "e2e-fixtures/included-skill"}`, repo.URL)))
	writeFixture(t, src, "with-pinned-include/.agents/harness/plugin.json", harnessManifest("with-pinned-include",
		"E2E fixture: a SHA-pinned include.",
		fmt.Sprintf(`{"git": %q, "path": "e2e-fixtures/minimal", "ref": %q}`, repo.URL, repo.TagSHA)))
	writeFixture(t, src, "with-tag-include/.agents/harness/plugin.json", harnessManifest("with-tag-include",
		"E2E fixture: a tag-pinned include.",
		fmt.Sprintf(`{"git": %q, "path": "e2e-fixtures/minimal", "ref": %q}`, repo.URL, fixtureTag)))
	mustGit(t, src, "add", "-A")
	mustGit(t, src, "commit", "--quiet", "-m", "remaining fixtures")
	repo.SHA = gitOutput(t, src, "rev-parse", "HEAD")

	mustGit(t, "", "clone", "--quiet", "--bare", src, bare)
	// ynh fetches a pinned commit by SHA, which a server refuses unless the
	// commit is advertised or this is set.
	mustGit(t, bare, "config", "uploadpack.allowReachableSHA1InWant", "true")

	mustGit(t, "", "clone", "--quiet", repo.URL, repo.Clone)
	mustGit(t, repo.Clone, "checkout", "--quiet", repo.SHA)
	return repo
}

// commit adds a commit to main in the bare repository that writes body to
// e2e-fixtures/<rel>, and returns its SHA. It works in a scratch clone, so
// repo.Clone is left where it was.
func (repo fixtureRepo) commit(t *testing.T, rel, body string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "push")
	mustGit(t, "", "clone", "--quiet", repo.URL, work)
	configIdentity(t, work)
	writeFixture(t, work, rel, body)
	mustGit(t, work, "add", "-A")
	mustGit(t, work, "commit", "--quiet", "-m", "update "+rel)
	mustGit(t, work, "push", "--quiet", "origin", "HEAD:main")
	return gitOutput(t, work, "rev-parse", "HEAD")
}

// harnessManifest returns a plugin.json body. include, when not empty, is
// the JSON of the manifest's single include.
func harnessManifest(name, description, include string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"$schema\": \"https://eyelock.github.io/ynh/schema/plugin.schema.json\",\n")
	fmt.Fprintf(&b, "  \"name\": %q,\n  \"version\": \"0.1.0\",\n  \"description\": %q,\n", name, description)
	b.WriteString(`  "default_vendor": "claude"`)
	if include != "" {
		fmt.Fprintf(&b, ",\n  \"includes\": [%s]", include)
	}
	b.WriteString("\n}\n")
	return b.String()
}

// initRepo makes dir a git repository on main with a fixed identity, so a
// developer's own git config cannot change what the fixtures commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "init", "--quiet", "--initial-branch=main")
	configIdentity(t, dir)
}

func configIdentity(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "config", "user.email", "e2e@example.invalid")
	mustGit(t, dir, "config", "user.name", "e2e")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	mustGit(t, dir, "config", "tag.gpgsign", "false")
}

// writeFixture writes body to e2e-fixtures/<rel> under repoDir.
func writeFixture(t *testing.T, repoDir, rel, body string) {
	t.Helper()
	path := filepath.Join(repoDir, "e2e-fixtures", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitOutput runs git in dir and returns its trimmed stdout.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}
