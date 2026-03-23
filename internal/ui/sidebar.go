package ui

import "github.com/adam/launch/internal/process"

// SidebarEntry represents one row in the sidebar: a group header, a section
// divider ("Tasks"), or a process/task item.
type SidebarEntry struct {
	IsGroup         bool
	IsSectionHeader bool
	Group           string
	Item            process.SidebarItem
	Hidden          bool // task is hidden by the user but showHiddenTasks is on
}

// buildSidebar constructs the ordered list of sidebar entries from the current
// manager state, respecting collapse/hide preferences.
func buildSidebar(manager *process.Manager, collapsedGroups map[string]bool, hiddenTasks map[string]bool, showHiddenTasks bool) []SidebarEntry {
	var sidebar []SidebarEntry

	groups := manager.GroupNames()
	multiGroup := len(groups) > 1

	for _, group := range groups {
		if multiGroup {
			sidebar = append(sidebar, SidebarEntry{IsGroup: true, Group: group})
		}
		if collapsedGroups[group] {
			continue
		}

		// Determine whether this group has any processes (affects task section header).
		hasProcesses := false
		for _, item := range manager.Items {
			if item.GetGroup() == group && item.Kind() == process.KindProcess {
				hasProcesses = true
				break
			}
		}

		addedTaskHeader := false
		for _, item := range manager.Items {
			if item.GetGroup() != group {
				continue
			}
			if item.Kind() == process.KindTask {
				if !addedTaskHeader {
					if hasProcesses {
						sidebar = append(sidebar, SidebarEntry{
							IsSectionHeader: true,
							Group:           group,
						})
					}
					addedTaskHeader = true
				}
				taskKey := item.GetGroup() + "/" + item.GetSlug()
				if hiddenTasks[taskKey] && !showHiddenTasks {
					continue
				}
				sidebar = append(sidebar, SidebarEntry{
					Group:  group,
					Item:   item,
					Hidden: hiddenTasks[taskKey],
				})
			} else {
				sidebar = append(sidebar, SidebarEntry{Group: group, Item: item})
			}
		}
	}

	return sidebar
}

// selectableIndices returns indices of all selectable entries (groups and items,
// not section dividers).
func selectableIndices(sidebar []SidebarEntry) []int {
	var indices []int
	for i, entry := range sidebar {
		if !entry.IsSectionHeader {
			indices = append(indices, i)
		}
	}
	return indices
}
