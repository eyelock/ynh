package sessions

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestKeyDistinguishesEveryField(t *testing.T) {
	base := Key("h", "/a", "n")
	for name, other := range map[string]string{
		"harness": Key("h2", "/a", "n"),
		"cwd":     Key("h", "/b", "n"),
		"name":    Key("h", "/a", "n2"),
		"shifted": Key("h/a", "", "n"),
	} {
		if other == base {
			t.Errorf("%s change did not change the key", name)
		}
	}
	if Key("h", "/a", "n") != base {
		t.Error("key is not stable")
	}
}

func TestSaveLoad(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	key := Key("h", "/a", "n")

	if _, ok, err := Load(key); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := Save(key, Binding{SessionID: "id-1", Name: "n", Cwd: "/a", Harness: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(key, Binding{SessionID: "id-2", Name: "n", Cwd: "/a", Harness: "h"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(key)
	if err != nil || !ok || got.SessionID != "id-2" || got.UpdatedAt.IsZero() {
		t.Fatalf("Load = %+v ok=%v err=%v", got, ok, err)
	}
	entries, _ := os.ReadDir(Dir())
	if len(entries) != 1 {
		t.Errorf("want one file and no temp leftovers, got %d", len(entries))
	}
}

func TestLoadMalformed(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(), "k.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load("k"); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, err := NewID()
	if err != nil || !re.MatchString(a) {
		t.Fatalf("id %q err %v", a, err)
	}
	if b, _ := NewID(); a == b {
		t.Error("ids repeat")
	}
}
