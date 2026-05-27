package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// GetDefaultInputPaths returns a list of discovered Minecraft saves directories
// for Vanilla, PrismLauncher, and ATLauncher based on the current OS.
func GetDefaultInputPaths() []string {
	var paths []string

	home := os.Getenv("HOME")
	appData := os.Getenv("APPDATA")
	localAppData := os.Getenv("LOCALAPPDATA")

	// Helper to add if directory exists
	addIfExists := func(dir string) bool {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			paths = append(paths, dir)
			return true
		}
		return false
	}

	// Helper to find saves inside an instances directory
	scanInstancesDir := func(instancesDir string) {
		entries, err := os.ReadDir(instancesDir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() {
				// Try <instance>/.minecraft/saves
				savesDir := filepath.Join(instancesDir, entry.Name(), ".minecraft", "saves")
				if !addIfExists(savesDir) {
					// Try <instance>/saves
					savesDirDirect := filepath.Join(instancesDir, entry.Name(), "saves")
					addIfExists(savesDirDirect)
				}
			}
		}
	}

	switch runtime.GOOS {
	case "windows":
		// Vanilla
		if appData != "" {
			addIfExists(filepath.Join(appData, ".minecraft", "saves"))
		}
		// PrismLauncher
		if appData != "" {
			scanInstancesDir(filepath.Join(appData, "PrismLauncher", "instances"))
		}
		if localAppData != "" {
			scanInstancesDir(filepath.Join(localAppData, "PrismLauncher", "instances"))
		}
		// ATLauncher
		if appData != "" {
			scanInstancesDir(filepath.Join(appData, "ATLauncher", "instances"))
		}
		if localAppData != "" {
			scanInstancesDir(filepath.Join(localAppData, "ATLauncher", "instances"))
		}
		// Also scan portable/default ATLauncher dir if exists
		scanInstancesDir("C:\\ATLauncher\\instances")

	case "darwin": // macOS
		if home != "" {
			appSupport := filepath.Join(home, "Library", "Application Support")
			// Vanilla
			addIfExists(filepath.Join(appSupport, "minecraft", "saves"))
			// PrismLauncher
			scanInstancesDir(filepath.Join(appSupport, "PrismLauncher", "instances"))
			// ATLauncher
			scanInstancesDir(filepath.Join(appSupport, "ATLauncher", "instances"))
		}

	case "linux":
		if home != "" {
			// Vanilla
			addIfExists(filepath.Join(home, ".minecraft", "saves"))
			// PrismLauncher standard
			scanInstancesDir(filepath.Join(home, ".local", "share", "PrismLauncher", "instances"))
			// PrismLauncher Flatpak
			scanInstancesDir(filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher", "data", "PrismLauncher", "instances"))
			// ATLauncher
			scanInstancesDir(filepath.Join(home, ".local", "share", "ATLauncher", "instances"))
			scanInstancesDir(filepath.Join(home, ".atlauncher", "instances"))
			scanInstancesDir(filepath.Join(home, "ATLauncher", "instances"))
		}
	}

	return paths
}
