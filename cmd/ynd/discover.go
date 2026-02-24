package main

import (
	"os"
	"path/filepath"
)

var skipDirs = map[string]bool{
	".git":         true,
	".terraform":   true,
	"node_modules": true,
	"vendor":       true,
}

// discoverFiles walks root and returns files matching the given extensions.
// If root is a file, it returns that single file regardless of extension.
func discoverFiles(root string, extensions []string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return []string{root}, nil
	}

	extSet := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		extSet[ext] = true
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(d.Name())
		if extSet[ext] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// discoverByName walks root and returns files matching the given exact names.
func discoverByName(root string, names []string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return []string{root}, nil
	}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if nameSet[d.Name()] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// discoverAll walks root and returns all files matching extensions or exact names.
func discoverAll(root string, extensions []string, names []string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return []string{root}, nil
	}

	extSet := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		extSet[ext] = true
	}
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(d.Name())
		if extSet[ext] || nameSet[d.Name()] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
