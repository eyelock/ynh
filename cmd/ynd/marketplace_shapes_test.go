package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func lintShape(t *testing.T, rel, body string) (marketplaceShape, []lintIssue) {
	t.Helper()
	path := filepath.Join(t.TempDir(), filepath.FromSlash(rel))
	writeFile(t, path, []byte(body))
	return classifyMarketplace(path), lintMarketplaceFile(path)
}

func TestMarketplaceShapes_BuildConfig(t *testing.T) {
	shape, issues := lintShape(t, "marketplace.json", `{"name":"m","owner":{"name":"o"},"harnesses":[{"type":"plugin","source":"./p"}]}`)
	if shape != shapeBuildConfig || len(issues) != 0 {
		t.Errorf("shape=%v issues=%v", shape, issues)
	}
	_, issues = lintShape(t, "marketplace.json", `{"name":"m","owner":{"name":"o"},"harnesses":[]}`)
	if len(issues) == 0 {
		t.Error("an empty build config must fail")
	}
}

func TestMarketplaceShapes_VendorIndex(t *testing.T) {
	good := `{"name":"m","owner":{"name":"o"},"plugins":[{"name":"a","source":"./a"},{"name":"b","source":{"source":"github","repo":"o/b"}}]}`
	for _, dir := range vendorIndexDirs {
		shape, issues := lintShape(t, dir+"/marketplace.json", good)
		if shape != shapeVendorIndex || len(issues) != 0 {
			t.Errorf("%s: shape=%v issues=%v", dir, shape, issues)
		}
	}
	_, issues := lintShape(t, ".claude-plugin/marketplace.json", `{"plugins":[{"source":"./a"}]}`)
	if len(issues) < 2 {
		t.Errorf("a vendor index missing name, plugin name expected to fail, got %v", issues)
	}
	// A vendor index is not mis-linted as a build config.
	for _, issue := range issues {
		if strings.Contains(issue.Message, "harness") || strings.Contains(issue.Message, "owner.name") {
			t.Errorf("linted as a build config: %s", issue.Message)
		}
	}
}

func TestMarketplaceShapes_CopilotRefusesUnsupportedSources(t *testing.T) {
	for _, kind := range unsupportedCopilotSources {
		body := `{"name":"m","plugins":[{"name":"a","source":{"source":"` + kind + `","url":"x"}}]}`
		_, issues := lintShape(t, ".github/plugin/marketplace.json", body)
		if len(issues) != 1 || !strings.Contains(issues[0].Message, kind) {
			t.Errorf("%s: issues = %v", kind, issues)
		}
		if _, issues := lintShape(t, ".claude-plugin/marketplace.json", body); len(issues) != 0 {
			t.Errorf("%s under .claude-plugin must pass, got %v", kind, issues)
		}
	}
}

func TestMarketplaceShapes_RegistryIndex(t *testing.T) {
	shape, issues := lintShape(t, ".agents/harness/marketplace.json", `{"name":"r"}`)
	if shape != shapeRegistry || len(issues) == 0 {
		t.Errorf("an invalid registry index must fail as one: shape=%v issues=%v", shape, issues)
	}
}

func TestMarketplaceShapes_UnclassifiableIsRefused(t *testing.T) {
	for _, body := range []string{`{"name":"m","plugins":[]}`, `not json`} {
		shape, issues := lintShape(t, "elsewhere/marketplace.json", body)
		if shape != shapeUnknown || len(issues) != 1 || !strings.Contains(issues[0].Message, "cannot tell which marketplace.json") {
			t.Errorf("%q: shape=%v issues=%v", body, shape, issues)
		}
	}
}
