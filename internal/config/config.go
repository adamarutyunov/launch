package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type Process struct {
	Command            string            `yaml:"command"`
	WorkingDir         *string           `yaml:"working_dir"`
	AutoStart          bool              `yaml:"auto_start"`
	AutoRestart        bool              `yaml:"auto_restart"`
	RestartWhenChanged []string          `yaml:"restart_when_changed"`
	Env                map[string]string `yaml:"env"`
}

type Config struct {
	Name      string             `yaml:"name"`
	Icon      *string            `yaml:"icon"`
	Processes map[string]Process `yaml:"processes"`
}

// Group represents a project with its processes, used for tree display.
type Group struct {
	Name       string
	ConfigPath string
	Processes  []NamedProcess
}

type NamedProcess struct {
	Name    string
	Process Process
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Resolve relative working dirs against config file location
	configDir := filepath.Dir(path)
	for name, proc := range cfg.Processes {
		if proc.WorkingDir != nil && *proc.WorkingDir != "" && !filepath.IsAbs(*proc.WorkingDir) {
			resolved := filepath.Join(configDir, *proc.WorkingDir)
			proc.WorkingDir = &resolved
			cfg.Processes[name] = proc
		}
	}

	// If no explicit working dir, default to config file directory
	for name, proc := range cfg.Processes {
		if proc.WorkingDir == nil || *proc.WorkingDir == "" {
			dir := configDir
			proc.WorkingDir = &dir
			cfg.Processes[name] = proc
		}
	}

	return &cfg, nil
}

// Discover finds all solo.yml/launch.yml files in the given directory.
// If the directory itself has a config, returns just that one.
// Otherwise, searches immediate subdirectories (one level deep).
func Discover(rootDir string) ([]Group, error) {
	configNames := []string{"solo.yml", "solo.yaml", "launch.yml", "launch.yaml"}

	// Check if root dir itself has a config
	for _, name := range configNames {
		path := filepath.Join(rootDir, name)
		if _, err := os.Stat(path); err == nil {
			cfg, err := Load(path)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", path, err)
			}
			return []Group{configToGroup(cfg, path)}, nil
		}
	}

	// Scan subdirectories
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", rootDir, err)
	}

	var groups []Group
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] == '.' {
			continue
		}

		subDir := filepath.Join(rootDir, entry.Name())
		for _, name := range configNames {
			path := filepath.Join(subDir, name)
			if _, err := os.Stat(path); err == nil {
				cfg, err := Load(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: skipping %s: %s\n", path, err)
					continue
				}
				groups = append(groups, configToGroup(cfg, path))
				break
			}
		}
	}

	if len(groups) == 0 {
		return nil, fmt.Errorf("no solo.yml or launch.yml found in %s or its subdirectories", rootDir)
	}

	// Sort groups by name
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups, nil
}

func configToGroup(cfg *Config, path string) Group {
	group := Group{
		Name:       cfg.Name,
		ConfigPath: path,
	}

	if group.Name == "" {
		group.Name = filepath.Base(filepath.Dir(path))
	}

	// Sort process names for stable ordering
	names := make([]string, 0, len(cfg.Processes))
	for name := range cfg.Processes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		group.Processes = append(group.Processes, NamedProcess{
			Name:    name,
			Process: cfg.Processes[name],
		})
	}

	return group
}
