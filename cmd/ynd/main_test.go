package main

import "testing"

func TestParseCompressArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantVendor string
		wantFiles  []string
		wantSkip   bool
		wantErr    bool
	}{
		{
			name: "no args",
			args: []string{},
		},
		{
			name:       "vendor flag short",
			args:       []string{"-v", "claude"},
			wantVendor: "claude",
		},
		{
			name:       "vendor flag long",
			args:       []string{"--vendor", "codex"},
			wantVendor: "codex",
		},
		{
			name:     "yes flag short",
			args:     []string{"-y"},
			wantSkip: true,
		},
		{
			name:     "yes flag long",
			args:     []string{"--yes"},
			wantSkip: true,
		},
		{
			name:       "vendor with file",
			args:       []string{"-v", "claude", "foo.md"},
			wantVendor: "claude",
			wantFiles:  []string{"foo.md"},
		},
		{
			name:       "all flags and files",
			args:       []string{"-v", "claude", "-y", "a.md", "b.md"},
			wantVendor: "claude",
			wantSkip:   true,
			wantFiles:  []string{"a.md", "b.md"},
		},
		{
			name:    "vendor missing value",
			args:    []string{"-v"},
			wantErr: true,
		},
		{
			name:    "unknown flag",
			args:    []string{"--unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vendor, files, skip, err := parseCompressArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if vendor != tt.wantVendor {
				t.Errorf("vendor = %q, want %q", vendor, tt.wantVendor)
			}
			if skip != tt.wantSkip {
				t.Errorf("skip = %v, want %v", skip, tt.wantSkip)
			}
			if len(files) != len(tt.wantFiles) {
				t.Errorf("files = %v, want %v", files, tt.wantFiles)
			} else {
				for i := range files {
					if files[i] != tt.wantFiles[i] {
						t.Errorf("files[%d] = %q, want %q", i, files[i], tt.wantFiles[i])
					}
				}
			}
		})
	}
}
