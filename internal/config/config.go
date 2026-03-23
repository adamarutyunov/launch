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

// Group represents a project with its processes and tasks, used for tree display.
type Group struct {
	Name       string
	ConfigPath string
	Processes  []NamedProcess
	Tasks      []NamedTask
}

type NamedProcess struct {
	Slug    string
	Process LaunchProcess
}

// NamedTask represents a task discovered from a Taskfile.
type NamedTask struct {
	Slug       string
	Desc       string // from Taskfile desc field; may be empty
	Command    string // e.g. "task build"
	WorkingDir string // absolute path to the directory containing the Taskfile
}

// taskfileTaskMinimal is the subset of a Taskfile task we care about.
type taskfileTaskMinimal struct {
	Desc string `yaml:"desc"`
}

// taskfileMinimal is the subset of a Taskfile we parse.
type taskfileMinimal struct {
	Tasks map[string]taskfileTaskMinimal `yaml:"tasks"`
}

func loadTaskfile(dir string) ([]NamedTask, error) {
	for _, name := range []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading taskfile: %w", err)
		}

		var tf taskfileMinimal
		if err := yaml.Unmarshal(data, &tf); err != nil {
			return nil, fmt.Errorf("parsing taskfile: %w", err)
		}

		slugs := make([]string, 0, len(tf.Tasks))
		for slug := range tf.Tasks {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)

		var tasks []NamedTask
		for _, slug := range slugs {
			task := tf.Tasks[slug]
			tasks = append(tasks, NamedTask{
				Slug:       slug,
				Desc:       task.Desc,
				Command:    "task " + slug,
				WorkingDir: dir,
			})
		}
		return tasks, nil
	}
	return nil, nil
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

	// No launch.yml in root — check for a standalone Taskfile in root.
	if tasks, _ := loadTaskfile(rootDir); len(tasks) > 0 {
		return []Group{{
			Name:  filepath.Base(rootDir),
			Tasks: tasks,
		}}, nil
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
		if err == nil {
			groups = append(groups, configToGroup(cfg, path))
			continue
		}

		// No launch.yml — check for a standalone Taskfile.
		tasks, _ := loadTaskfile(subDir)
		if len(tasks) > 0 {
			groups = append(groups, Group{
				Name:  entry.Name(),
				Tasks: tasks,
			})
		}
	}

	if len(groups) == 0 {
		return nil, fmt.Errorf("no launch.yml or Taskfile found in %s or its subdirectories", rootDir)
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

	group.Tasks, _ = loadTaskfile(filepath.Dir(path))

	return group
}
