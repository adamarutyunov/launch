package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adam/launch/internal/config"
	"github.com/adam/launch/internal/process"
	"github.com/adam/launch/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	rootDir := "."
	if len(os.Args) > 1 {
		rootDir = os.Args[1]
	}

	absDir, err := filepath.Abs(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	groups, err := config.Discover(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	manager := process.NewManager()

	for _, group := range groups {
		for _, namedProc := range group.Processes {
			workingDir := ""
			if namedProc.Process.WorkingDir != nil {
				workingDir = *namedProc.Process.WorkingDir
			}
			managed := process.NewManagedProcess(
				namedProc.Name,
				group.Name,
				namedProc.Process.Command,
				workingDir,
				namedProc.Process.Env,
				namedProc.Process.AutoStart,
			)
			manager.Add(managed)
		}
	}

	title := filepath.Base(absDir)
	if len(groups) == 1 {
		title = groups[0].Name
	}

	model := ui.NewModel(manager, title)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Set program reference before Run so Init() and processes can use it
	manager.Program = program

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	manager.StopAll()
}
