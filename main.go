package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adamarutyunov/launch/internal/config"
	"github.com/adamarutyunov/launch/internal/process"
	"github.com/adamarutyunov/launch/internal/state"
	"github.com/adamarutyunov/launch/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// teaNotifier adapts *tea.Program to the process.Notifier interface.
type teaNotifier struct{ prog *tea.Program }

func (n teaNotifier) Send(msg any) { n.prog.Send(msg) }

func main() {
	noAutoStart := flag.Bool("no-autostart", false, "skip auto-starting processes on launch")
	flag.Parse()
	rootDir := "."
	if flag.NArg() > 0 {
		rootDir = flag.Arg(0)
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

	manager := process.NewManager(absDir)

	for _, group := range groups {
		for _, namedProc := range group.Processes {
			workingDir := ""
			if namedProc.Process.WorkingDir != nil {
				workingDir = *namedProc.Process.WorkingDir
			}
			logFile := state.LogFilePath(absDir, group.Name, namedProc.Slug)
			managed := process.NewManagedProcess(
				namedProc.Slug,
				namedProc.Process.Title,
				group.Name,
				namedProc.Process.Command,
				workingDir,
				namedProc.Process.Env,
				namedProc.Process.AutoStart,
				namedProc.Process.DependsOn,
				namedProc.Process.ReadyCheck,
				logFile,
			)
			manager.Add(managed)
		}
		for _, namedTask := range group.Tasks {
			managed := process.NewManagedTask(
				namedTask.Slug,
				namedTask.Desc,
				group.Name,
				namedTask.Command,
				namedTask.WorkingDir,
			)
			manager.AddTask(managed)
		}
	}

	settings := state.LoadSettings(absDir)
	model := ui.NewModel(manager, "Launch "+Version, settings)
	model.NoAutoStart = *noAutoStart

	session, err := state.Load(absDir)
	if err == nil && len(session.Processes) > 0 {
		model.SavedSession = session
	}

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	manager.SetNotifier(teaNotifier{program})

	finalModel, err := program.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	if m, ok := finalModel.(ui.Model); ok {
		switch m.ExitMode {
		case ui.ExitDetach:
			if err := manager.SaveState(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save state: %s\n", err)
			}
			fmt.Println("Detached. Processes are still running. Run 'launch' again to reattach.")
		case ui.ExitKill:
			manager.StopAll()
			fmt.Println("All processes stopped.")
		}
	} else {
		manager.StopAll()
	}
}
