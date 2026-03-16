package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ReadyCheck struct {
	Command  string `yaml:"command"`
	Interval string `yaml:"interval"` // e.g. "2s", "500ms"
	Retries  int    `yaml:"retries"`
}

func (r ReadyCheck) IntervalDuration() time.Duration {
	if r.Interval == "" {
		return 2 * time.Second
	}
	d, err := time.ParseDuration(r.Interval)
	if err != nil {
		return 2 * time.Second
	}
	return d
}

// LaunchProcess is the canonical process definition (launch.yml format).
type LaunchProcess struct {
	Title      string            `yaml:"title"`
	Command    string            `yaml:"command"`
	WorkingDir *string           `yaml:"working_dir"`
	AutoStart  bool              `yaml:"auto_start"`
	Env        map[string]string `yaml:"env"`
	DependsOn  []string          `yaml:"depends_on"`
	ReadyCheck *ReadyCheck       `yaml:"ready_check"`
}

// LaunchConfig is the canonical config format (launch.yml).
type LaunchConfig struct {
	Name      string                   `yaml:"name"`
	Processes map[string]LaunchProcess `yaml:"processes"`
}

// soloProcess is the Solo app's process format (solo.yml fallback).
type soloProcess struct {
	Command            string            `yaml:"command"`
	WorkingDir         *string           `yaml:"working_dir"`
	AutoStart          bool              `yaml:"auto_start"`
	AutoRestart        bool              `yaml:"auto_restart"`
	RestartWhenChanged []string          `yaml:"restart_when_changed"`
	Env                map[string]string `yaml:"env"`
}

type soloConfig struct {
	Name      string                 `yaml:"name"`
	Icon      *string                `yaml:"icon"`
	Processes map[string]soloProcess `yaml:"processes"`
}

// Group represents a project with its processes, used for tree display.
type Group struct {
	Name       string
	ConfigPath string
	Processes  []NamedProcess
}

type NamedProcess struct {
	Slug    string
	Process LaunchProcess
}

func loadLaunchConfig(path string) (*LaunchConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg LaunchConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	resolveWorkingDirs(&cfg, path)
	return &cfg, nil
}

func loadSoloConfig(path string) (*LaunchConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var solo soloConfig
	if err := yaml.Unmarshal(data, &solo); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &LaunchConfig{
		Name:      solo.Name,
		Processes: make(map[string]LaunchProcess, len(solo.Processes)),
	}

	for name, proc := range solo.Processes {
		slug := nameToSlug(name)
		cfg.Processes[slug] = LaunchProcess{
			Title:      name,
			Command:    proc.Command,
			WorkingDir: proc.WorkingDir,
			AutoStart:  proc.AutoStart,
			Env:        proc.Env,
		}
	}

	resolveWorkingDirs(cfg, path)
	return cfg, nil
}

func nameToSlug(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "_")
	slug = strings.ReplaceAll(slug, "-", "_")
	return slug
}

func resolveWorkingDirs(cfg *LaunchConfig, configPath string) {
	configDir := filepath.Dir(configPath)

	for slug, proc := range cfg.Processes {
		if proc.WorkingDir != nil && *proc.WorkingDir != "" && !filepath.IsAbs(*proc.WorkingDir) {
			resolved := filepath.Join(configDir, *proc.WorkingDir)
			proc.WorkingDir = &resolved
		}
		if proc.WorkingDir == nil || *proc.WorkingDir == "" {
			dir := configDir
			proc.WorkingDir = &dir
		}
		if proc.Title == "" {
			proc.Title = slug
		}
		cfg.Processes[slug] = proc
	}
}

func loadConfig(dir string) (*LaunchConfig, string, error) {
	for _, name := range []string{"launch.yml", "launch.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			cfg, err := loadLaunchConfig(path)
			return cfg, path, err
		}
	}

	for _, name := range []string{"solo.yml", "solo.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			cfg, err := loadSoloConfig(path)
			return cfg, path, err
		}
	}

	return nil, "", fmt.Errorf("no launch.yml found in %s", dir)
}

// Discover finds all config files in the given directory or its subdirectories.
func Discover(rootDir string) ([]Group, error) {
	cfg, path, err := loadConfig(rootDir)
	if err == nil {
		return []Group{configToGroup(cfg, path)}, nil
	}

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
		cfg, path, err := loadConfig(subDir)
		if err != nil {
			continue
		}
		groups = append(groups, configToGroup(cfg, path))
	}

	if len(groups) == 0 {
		return nil, fmt.Errorf("no launch.yml found in %s or its subdirectories", rootDir)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups, nil
}

func configToGroup(cfg *LaunchConfig, path string) Group {
	group := Group{
		Name:       cfg.Name,
		ConfigPath: path,
	}

	if group.Name == "" {
		group.Name = filepath.Base(filepath.Dir(path))
	}

	slugs := make([]string, 0, len(cfg.Processes))
	for slug := range cfg.Processes {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	for _, slug := range slugs {
		group.Processes = append(group.Processes, NamedProcess{
			Slug:    slug,
			Process: cfg.Processes[slug],
		})
	}

	return group
}
