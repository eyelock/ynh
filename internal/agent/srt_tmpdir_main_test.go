package agent

import (
	"os"
	"testing"
)

// TestMain points srt's temporary directory at a scratch folder, so no test
// creates /tmp/claude on the machine that runs them.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ynh-srt-tmp-")
	if err != nil {
		panic(err)
	}
	srtTmpdir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
