package vendor

import "testing"

func TestCodexDelegateAgentFile(t *testing.T) {
	c := &Codex{}
	name, data, err := c.DelegateAgentFile("probe", "A \"quoted\" one\nover two lines", "Line one.\n\tTabbed \"\"\" C:\\x \x01 end")
	if err != nil {
		t.Fatal(err)
	}
	if name != "probe.toml" {
		t.Errorf("file = %q, want probe.toml", name)
	}
	want := "name = \"probe\"\n" +
		"description = \"A \\\"quoted\\\" one\\nover two lines\"\n" +
		"developer_instructions = \"\"\"\nLine one.\n\tTabbed \\\"\\\"\\\" C:\\\\x \\u0001 end\"\"\"\n"
	if string(data) != want {
		t.Errorf("got\n%s\nwant\n%s", data, want)
	}
}

func TestCodexDelegateAgentFile_RefusesPathNames(t *testing.T) {
	c := &Codex{}
	for _, n := range []string{"", ".", "..", "a/b", `a\b`} {
		if _, _, err := c.DelegateAgentFile(n, "d", "p"); err == nil {
			t.Errorf("name %q: want an error", n)
		}
	}
}
