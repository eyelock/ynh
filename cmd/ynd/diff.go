package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

func cmdDiff(args []string) error {
	var (
		source      string
		profileName string
		vendors     []string
	)

	// Parse: source is first positional, remaining positional args are vendor names
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v", "--vendor":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", args[i])
			}
			i++
			for _, v := range strings.Split(args[i], ",") {
				vendors = append(vendors, strings.TrimSpace(v))
			}
		case "--harness":
			if i+1 >= len(args) {
				return fmt.Errorf("--harness requires a value")
			}
			i++
			source = args[i]
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			profileName = args[i]
		case "-h", "--help":
			return errHelp
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			if source == "" {
				source = args[i]
			} else {
				vendors = append(vendors, args[i])
			}
		}
	}

	// Resolve source: --harness flag > YNH_HARNESS > positional > error
	if source == "" {
		source = resolveHarnessEnv()
	}
	if source == "" {
		return fmt.Errorf("usage: ynd diff <harness-dir> [--harness dir] [-v vendor1,vendor2] [--profile name] [vendor1 vendor2 ...]")
	}

	// Resolve profile from flag or env var
	if profileName == "" {
		profileName = os.Getenv("YNH_PROFILE")
	}

	// Resolve source
	srcDir, err := resolveSource(source)
	if err != nil {
		return err
	}

	// Default to all vendors
	if len(vendors) == 0 {
		vendors = vendor.Available()
	}

	// Validate vendor names
	for _, v := range vendors {
		if _, err := vendor.Get(v); err != nil {
			return err
		}
	}

	if len(vendors) < 2 {
		return fmt.Errorf("diff requires at least 2 vendors to compare")
	}

	// Assemble each vendor into a temp dir
	type vendorResult struct {
		name string
		dir  string
	}
	var results []vendorResult
	defer func() {
		for _, r := range results {
			_ = os.RemoveAll(r.dir)
		}
	}()

	for _, v := range vendors {
		tmpDir, err := assembleForVendor(srcDir, v, profileName)
		if err != nil {
			return fmt.Errorf("assembling for %s: %w", v, err)
		}
		results = append(results, vendorResult{name: v, dir: tmpDir})
	}

	// Compare each pair
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			a := results[i]
			b := results[j]

			fmt.Printf("=== %s vs %s ===\n", a.name, b.name)

			filesA, err := listFiles(a.dir)
			if err != nil {
				return fmt.Errorf("listing files for %s: %w", a.name, err)
			}
			filesB, err := listFiles(b.dir)
			if err != nil {
				return fmt.Errorf("listing files for %s: %w", b.name, err)
			}

			setA := make(map[string]bool, len(filesA))
			for _, f := range filesA {
				setA[f] = true
			}
			setB := make(map[string]bool, len(filesB))
			for _, f := range filesB {
				setB[f] = true
			}

			// Only in A
			var onlyA []string
			for _, f := range filesA {
				if !setB[f] {
					onlyA = append(onlyA, f)
				}
			}

			// Only in B
			var onlyB []string
			for _, f := range filesB {
				if !setA[f] {
					onlyB = append(onlyB, f)
				}
			}

			// In both — check content
			var different []string
			var same []string
			for _, f := range filesA {
				if setB[f] {
					dataA, errA := os.ReadFile(filepath.Join(a.dir, f))
					dataB, errB := os.ReadFile(filepath.Join(b.dir, f))
					if errA != nil || errB != nil {
						different = append(different, f)
						continue
					}
					if string(dataA) != string(dataB) {
						different = append(different, f)
					} else {
						same = append(same, f)
					}
				}
			}

			if len(onlyA) > 0 {
				fmt.Printf("Only in %s:\n", a.name)
				for _, f := range onlyA {
					fmt.Printf("  %s\n", f)
				}
			}

			if len(onlyB) > 0 {
				fmt.Printf("Only in %s:\n", b.name)
				for _, f := range onlyB {
					fmt.Printf("  %s\n", f)
				}
			}

			if len(different) > 0 {
				fmt.Println("Different content:")
				for _, f := range different {
					fmt.Printf("  %s\n", f)
				}
			}

			if len(same) > 0 {
				fmt.Println("Identical:")
				for _, f := range same {
					fmt.Printf("  %s\n", f)
				}
			}

			fmt.Println()
		}
	}

	return nil
}

// listFiles returns all file paths relative to root, sorted.
func listFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	sort.Strings(files)
	return files, nil
}
