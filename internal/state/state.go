package state

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ProcessState struct {
	PID        int       `json:"pid"`
	Group      string    `json:"group"`
	Name       string    `json:"name"`
	Command    string    `json:"command"`
	WorkingDir string    `json:"working_dir"`
	LogFile    string    `json:"log_file"`
	StartedAt  time.Time `json:"started_at"`
}

type SessionState struct {
	RootDir   string                  `json:"root_dir"`
	Processes map[string]ProcessState `json:"processes"`
}

func sessionID(rootDir string) string {
	hash := sha256.Sum256([]byte(rootDir))
	return fmt.Sprintf("%x", hash[:6])
}

func baseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".launch")
}

func StateFilePath(rootDir string) string {
	return filepath.Join(baseDir(), "state", sessionID(rootDir)+".json")
}

func LogDir(rootDir string) string {
	return filepath.Join(baseDir(), "logs", sessionID(rootDir))
}

func LogFilePath(rootDir, group, name string) string {
	// Sanitize group/name for filesystem
	safeGroup := sanitize(group)
	safeName := sanitize(name)
	return filepath.Join(LogDir(rootDir), safeGroup, safeName+".log")
}

func sanitize(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == '\\' || c == ':' || c == 0 {
			result = append(result, '_')
		} else {
			result = append(result, c)
		}
	}
	return string(result)
}

func Load(rootDir string) (*SessionState, error) {
	path := StateFilePath(rootDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionState{
				RootDir:   rootDir,
				Processes: make(map[string]ProcessState),
			}, nil
		}
		return nil, fmt.Errorf("reading state: %w", err)
	}

	var session SessionState
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}

	if session.Processes == nil {
		session.Processes = make(map[string]ProcessState)
	}

	return &session, nil
}

func Save(session *SessionState) error {
	path := StateFilePath(session.RootDir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

func Remove(rootDir string) error {
	path := StateFilePath(rootDir)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ProcessKey returns the unique key for a process in the state map.
func ProcessKey(group, name string) string {
	return group + "/" + name
}

