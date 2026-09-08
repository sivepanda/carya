package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"carya/internal/chunk"
	"carya/internal/git"
	"carya/internal/tui"
	"carya/internal/tui/shared"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
)

var forceJump bool
var jumpUserFlag string

type jumpState struct {
	TargetUser string `json:"target_user"`
	StashHash  string `json:"stash_hash"`
}

var jumpCmd = &cobra.Command{
	Use:   "jump [user]",
	Short: "Jump into another user's working state",
	Long: `Checkout another team member's working state into your working directory.

This will modify files in your working directory to match the target user's state.
If you have local changes, Carya will stash them automatically before jumping.

Use --force to skip the confirmation prompt.
Use --user if the username conflicts with a subcommand name.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetUser, err := resolveJumpUser(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if targetUser == "" {
			_ = cmd.Help()
			return
		}

		repo := mustInitializedRepo()
		if jumpInProgress(repo.CaryaPath()) {
			fmt.Fprintln(os.Stderr, "Error: A jump is already active. Run 'carya jump leave' before jumping again.")
			os.Exit(1)
		}

		refManager := git.NewRefManager(repo.RootPath())
		treeHash, err := refManager.GetUserTreeRef(targetUser)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: User '%s' hasn't published their state.\n", targetUser)
			fmt.Fprintf(os.Stderr, "Use 'carya team --fetch' to get the latest refs from origin.\n")
			os.Exit(1)
		}

		fmt.Printf("Jumping to %s's working state (tree: %s)\n", targetUser, shortHash(treeHash))

		headTree, err := refManager.GetHEADTreeHash()
		if err == nil {
			predictor := git.NewConflictPredictor(repo.RootPath())
			diffs, err := predictor.DiffTrees(headTree, treeHash)
			if err == nil && len(diffs) > 0 {
				fmt.Println("\nFiles that will be modified:")
				for _, d := range diffs {
					status := "?"
					switch d.Status {
					case "A":
						status = "add"
					case "D":
						status = "del"
					case "M":
						status = "mod"
					}
					fmt.Printf("  [%s] %s\n", status, d.Path)
				}
				fmt.Println()
			}
		}

		hasChanges, err := hasLocalChanges(repo.RootPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking local changes: %v\n", err)
			os.Exit(1)
		}

		if hasChanges {
			fmt.Println("Detected local changes: they will be stashed before jumping.")
		}

		if !forceJump {
			fmt.Print("This will modify your working directory. Continue? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				fmt.Println("Aborted.")
				os.Exit(0)
			}
		}

		stashHash := ""
		if hasChanges {
			hash, err := stashLocalChanges(repo.RootPath(), targetUser)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error stashing local changes: %v\n", err)
				os.Exit(1)
			}
			stashHash = hash
		}
		if err := writeJumpState(repo.CaryaPath(), jumpState{TargetUser: targetUser, StashHash: stashHash}); err != nil {
			if stashHash != "" {
				_ = restoreStash(repo.RootPath(), stashHash)
			}
			fmt.Fprintf(os.Stderr, "Error recording jump state: %v\n", err)
			os.Exit(1)
		}

		if err := checkoutTree(repo.RootPath(), treeHash); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking out tree: %v\n", err)
			// Roll back so a failed jump doesn't leave stale state blocking
			// future jumps. If restoring HEAD itself fails, keep the state
			// file so 'carya jump leave' can still recover.
			if restoreErr := checkoutTree(repo.RootPath(), "HEAD"); restoreErr != nil {
				fmt.Fprintf(os.Stderr, "Error restoring HEAD state: %v\n", restoreErr)
				fmt.Fprintln(os.Stderr, "Run 'carya jump leave' to recover your previous state.")
				os.Exit(1)
			}
			if stashHash != "" {
				if err := restoreStash(repo.RootPath(), stashHash); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
					fmt.Fprintf(os.Stderr, "  Recover manually: git stash apply %s\n", shortHash(stashHash))
				}
			}
			if err := clearJumpState(repo.CaryaPath()); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not clear jump state: %v\n", err)
			}
			os.Exit(1)
		}

		fmt.Printf("\nSuccessfully jumped to %s's working state.\n", targetUser)
		fmt.Println("Your working directory now reflects their changes.")
		if stashHash != "" {
			fmt.Println("\nYour local changes were stashed before the jump.")
			fmt.Printf("  Stash: %s\n", shortHash(stashHash))
		}
		fmt.Println("\nTo return to your previous state and restore stashed changes:")
		fmt.Println("  carya jump leave")
		fmt.Println("\nTo inspect changes without modifying files:")
		fmt.Printf("  carya jump view %s\n", targetUser)
	},
}

var jumpLeaveCmd = &cobra.Command{
	Use:   "leave",
	Short: "Return from jumped state",
	Long:  `Restore your working directory to HEAD and pop any auto-created jump stash.`,
	Run: func(cmd *cobra.Command, args []string) {
		repo := mustInitializedRepo()

		state, err := readJumpState(repo.CaryaPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading jump state: %v\n", err)
			os.Exit(1)
		}

		if err := checkoutTree(repo.RootPath(), "HEAD"); err != nil {
			fmt.Fprintf(os.Stderr, "Error restoring HEAD state: %v\n", err)
			os.Exit(1)
		}

		if state.StashHash != "" {
			if err := restoreStash(repo.RootPath(), state.StashHash); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			}
		}

		if err := clearJumpState(repo.CaryaPath()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not clear jump state: %v\n", err)
		}

		fmt.Println("Returned to HEAD state.")
	},
}

var jumpViewCmd = &cobra.Command{
	Use:   "view <user>",
	Short: "View teammate changes in read-only TUI",
	Long:  `Open a read-only TUI preview of differences between your HEAD and a teammate's published state.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetUser := args[0]

		repo := mustInitializedRepo()

		refManager := git.NewRefManager(repo.RootPath())
		treeHash, err := refManager.GetUserTreeRef(targetUser)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: User '%s' hasn't published their state.\n", targetUser)
			fmt.Fprintf(os.Stderr, "Use 'carya team --fetch' to get the latest refs from origin.\n")
			os.Exit(1)
		}

		headTree, err := refManager.GetHEADTreeHash()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading HEAD tree: %v\n", err)
			os.Exit(1)
		}

		entries, err := loadPreviewEntries(repo.RootPath(), headTree, treeHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading preview: %v\n", err)
			os.Exit(1)
		}

		p := tea.NewProgram(newJumpPreviewModel(targetUser, entries))
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running preview: %v\n", err)
			os.Exit(1)
		}
	},
}

type previewEntry struct {
	Path   string
	Status string
	Patch  string
}

type jumpPreviewModel struct {
	targetUser string
	entries    []previewEntry
	cursor     int
	filtered   []int
	list       viewport.Model
	diff       viewport.Model
	search     textinput.Model
	searching  bool
	ready      bool
	width      int
	height     int
	listWidth  int
	diffWidth  int
}

func newJumpPreviewModel(targetUser string, entries []previewEntry) jumpPreviewModel {
	search := textinput.New()
	search.Prompt = "search: "
	search.Placeholder = "type file path..."
	search.CharLimit = 200
	search.SetWidth(36)

	model := jumpPreviewModel{targetUser: targetUser, entries: entries, width: 80, height: 24, search: search}
	model.rebuildFilter()
	return model
}

func (m jumpPreviewModel) Init() tea.Cmd { return nil }

func (m jumpPreviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		layout := shared.CalculateSplitViewLayout(msg.Width, msg.Height, 2, 2)
		m.listWidth = layout.ListWidth
		m.diffWidth = layout.DiffWidth
		if !m.ready {
			m.list, m.diff = shared.InitializeViewports(layout)
			m.ready = true
		} else {
			shared.UpdateViewportSizes(&m.list, &m.diff, layout)
		}
		m.resizeViewports()
		m.refreshContent()
		return m, nil
	case tea.KeyMsg:
		nav := shared.HandleSplitPaneNavigation(
			msg,
			&m.searching,
			&m.search,
			&m.cursor,
			len(m.filtered),
			&m.diff,
			shared.IsUpKey(msg),
			shared.IsDownKey(msg),
			func() { m.rebuildFilter() },
		)
		if nav.Handled {
			if nav.NeedsRefresh {
				m.refreshContent()
			}
			return m, nav.Cmd
		}

		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m jumpPreviewModel) View() tea.View {
	var s string

	if !m.ready {
		loadingText := tui.TextStyle.Render(" Loading preview...")
		s = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, loadingText)
	} else if len(m.entries) == 0 {
		title := tui.TitleStyle.Render("JUMP PREVIEW")
		emptyMsg := tui.SubtleTextStyle.Render("No differences")
		help := tui.HelpDescStyle.Render("q quit")
		content := lipgloss.JoinVertical(lipgloss.Center, title, "", emptyMsg, "", help)
		s = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	} else {
		title := tui.TitleStyle.Render("JUMP PREVIEW (read-only)")
		subtitle := tui.SubtleTextStyle.Render("Comparing your HEAD against " + m.targetUser + "'s published state")
		legendMinus := lipgloss.NewStyle().Foreground(tui.ColorError).Bold(true).Render("- YOUR HEAD")
		legendPlus := lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("+ " + strings.ToUpper(m.targetUser) + "")
		legend := tui.SubtleTextStyle.Render("Legend: ") + legendMinus + tui.SubtleTextStyle.Render("  |  ") + legendPlus

		searchLine := shared.RenderSearchBar(m.searching, m.search.View(), m.search.Value(), len(m.filtered), len(m.entries), m.width)
		paneHeight := m.height - lipgloss.Height(title) - lipgloss.Height(subtitle) - lipgloss.Height(legend) - lipgloss.Height(searchLine) - lipgloss.Height(m.helpLine())
		if paneHeight < 1 {
			paneHeight = 1
		}

		left := shared.RenderTitledPanel("FILES", m.list.View(), m.listWidth, paneHeight, tui.ColorBorder)
		right := shared.RenderTitledPanel("DIFF", m.diff.View(), m.diffWidth, paneHeight, tui.ColorTitle)

		help := m.helpLine()

		body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
		s = lipgloss.JoinVertical(lipgloss.Left, title, subtitle, legend, searchLine, body, help)
	}

	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func (m jumpPreviewModel) helpLine() string {
	return tui.HelpKeyStyle.Render("j/k") + tui.HelpDescStyle.Render(" navigate") +
		tui.HelpDescStyle.Render(" • ") +
		tui.HelpKeyStyle.Render("/") + tui.HelpDescStyle.Render(" search") +
		tui.HelpDescStyle.Render(" • ") +
		tui.HelpKeyStyle.Render("ctrl+d/u") + tui.HelpDescStyle.Render(" scroll") +
		tui.HelpDescStyle.Render(" • ") +
		tui.HelpKeyStyle.Render("q") + tui.HelpDescStyle.Render(" quit")
}

func (m *jumpPreviewModel) resizeViewports() {
	if !m.ready {
		return
	}

	title := tui.TitleStyle.Render("JUMP PREVIEW (read-only)")
	subtitle := tui.SubtleTextStyle.Render("Comparing your HEAD against " + m.targetUser + "'s published state")
	legendMinus := lipgloss.NewStyle().Foreground(tui.ColorError).Bold(true).Render("- YOUR HEAD")
	legendPlus := lipgloss.NewStyle().Foreground(tui.ColorSuccess).Bold(true).Render("+ " + strings.ToUpper(m.targetUser) + "")
	legend := tui.SubtleTextStyle.Render("Legend: ") + legendMinus + tui.SubtleTextStyle.Render("  |  ") + legendPlus
	searchLine := shared.RenderSearchBar(m.searching, m.search.View(), m.search.Value(), len(m.filtered), len(m.entries), m.width)

	paneHeight := m.height - lipgloss.Height(title) - lipgloss.Height(subtitle) - lipgloss.Height(legend) - lipgloss.Height(searchLine) - lipgloss.Height(m.helpLine())
	if paneHeight < 1 {
		paneHeight = 1
	}

	listWidth, listHeight := shared.TitledPanelViewportSize(m.listWidth, paneHeight, 0)
	diffWidth, diffHeight := shared.TitledPanelViewportSize(m.diffWidth, paneHeight, 0)
	m.list.SetWidth(listWidth)
	m.list.SetHeight(listHeight)
	m.diff.SetWidth(diffWidth)
	m.diff.SetHeight(diffHeight)
}

func (m *jumpPreviewModel) refreshContent() {
	if !m.ready {
		return
	}

	if len(m.entries) == 0 {
		m.list.SetContent("No differences")
		m.diff.SetContent("No changes between HEAD and target state.")
		return
	}
	if len(m.filtered) == 0 {
		m.list.SetContent(tui.SubtleTextStyle.Render("No files match filter"))
		m.diff.SetContent(tui.SubtleTextStyle.Render("Update your search query to see matching files."))
		return
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	items := make([]string, 0, len(m.filtered))
	for visibleIndex, originalIndex := range m.filtered {
		e := m.entries[originalIndex]
		prefix := "  "
		if visibleIndex == m.cursor {
			prefix = tui.IconCursor + " "
		}

		status := e.Status
		switch e.Status {
		case "A":
			status = lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("A")
		case "D":
			status = lipgloss.NewStyle().Foreground(tui.ColorError).Render("D")
		case "M":
			status = lipgloss.NewStyle().Foreground(tui.ColorWarning).Render("M")
		}

		line := fmt.Sprintf("%s[%s] %s", prefix, status, e.Path)
		if visibleIndex == m.cursor {
			line = tui.SelectedItemStyle.Render(line)
		} else {
			line = tui.ItemStyle.Render(line)
		}
		items = append(items, line)
	}
	m.list.SetContent(strings.Join(items, "\n"))
	shared.EnsureItemVisible(&m.list, m.cursor)

	selected := m.entries[m.filtered[m.cursor]]
	content := selected.Patch
	if strings.TrimSpace(content) == "" {
		content = "No patch content available."
	}
	m.diff.SetContent(chunk.FormatDiff(content))
	m.diff.GotoTop()
}

func (m *jumpPreviewModel) rebuildFilter() {
	paths := make([]string, len(m.entries))
	for i, e := range m.entries {
		paths[i] = e.Path
	}
	m.filtered = shared.BuildFilteredIndices(paths, m.search.Value())
	if len(m.filtered) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
}

func resolveJumpUser(args []string) (string, error) {
	if jumpUserFlag != "" {
		if len(args) > 0 {
			return "", fmt.Errorf("provide either positional <user> or --user, not both")
		}
		return jumpUserFlag, nil
	}
	if len(args) == 0 {
		return "", nil
	}
	return args[0], nil
}

func hasLocalChanges(repoPath string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(string(output)) != "", nil
}

func stashLocalChanges(repoPath, targetUser string) (string, error) {
	msg := fmt.Sprintf("carya jump backup before jumping to %s at %s", targetUser, time.Now().Format(time.RFC3339))
	cmd := exec.Command("git", "stash", "push", "--include-untracked", "-m", msg)
	cmd.Dir = repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to stash changes: %w\nOutput: %s", err, output)
	}

	hash, err := stashCommitHash(repoPath, "stash@{0}")
	if err != nil {
		return "", err
	}
	if hash == "" {
		return "", fmt.Errorf("stash did not create a new entry")
	}

	return hash, nil
}

func stashCommitHash(repoPath, stashRef string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "-q", stashRef)
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func stashRefForHash(repoPath, stashHash string) (string, error) {
	cmd := exec.Command("git", "stash", "list", "--format=%gd %H")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if parts[1] == stashHash {
			return parts[0], nil
		}
	}

	return "", nil
}

func restoreStash(repoPath, stashHash string) error {
	stashRef, err := stashRefForHash(repoPath, stashHash)
	if err != nil {
		return fmt.Errorf("could not locate jump stash: %w", err)
	}

	if stashRef != "" {
		popCmd := exec.Command("git", "stash", "pop", stashRef)
		popCmd.Dir = repoPath
		popCmd.Stdout = os.Stdout
		popCmd.Stderr = os.Stderr
		if err := popCmd.Run(); err != nil {
			return fmt.Errorf("could not auto-pop stash %s: %w", shortHash(stashHash), err)
		}
		return nil
	}

	if objectExists(repoPath, stashHash) {
		applyCmd := exec.Command("git", "stash", "apply", stashHash)
		applyCmd.Dir = repoPath
		applyCmd.Stdout = os.Stdout
		applyCmd.Stderr = os.Stderr
		if err := applyCmd.Run(); err != nil {
			return fmt.Errorf("stash object %s exists but apply failed: %w", shortHash(stashHash), err)
		}
		return nil
	}

	return fmt.Errorf("jump stash %s was not found", shortHash(stashHash))
}

func objectExists(repoPath, hash string) bool {
	cmd := exec.Command("git", "cat-file", "-e", hash)
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

func checkoutTree(repoPath, treeish string) error {
	cmd := exec.Command("git", "checkout", treeish, "--", ".")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	if !strings.Contains(string(output), "did not match any file") {
		return fmt.Errorf("%w\nOutput: %s", err, output)
	}

	treeCmd := exec.Command("git", "rev-parse", treeish+"^{tree}")
	treeCmd.Dir = repoPath
	treeOut, treeErr := treeCmd.Output()
	if treeErr != nil {
		return fmt.Errorf("%w\nOutput: %s", err, output)
	}
	treeHash := strings.TrimSpace(string(treeOut))

	lsTreeCmd := exec.Command("git", "ls-tree", "-r", "--name-only", treeHash)
	lsTreeCmd.Dir = repoPath
	lsOut, lsErr := lsTreeCmd.Output()
	if lsErr != nil {
		return fmt.Errorf("%w\nOutput: %s", err, output)
	}
	if strings.TrimSpace(string(lsOut)) != "" {
		return fmt.Errorf("%w\nOutput: %s", err, output)
	}

	lsFilesCmd := exec.Command("git", "ls-files", "-z")
	lsFilesCmd.Dir = repoPath
	trackedOut, trackedErr := lsFilesCmd.Output()
	if trackedErr != nil {
		return trackedErr
	}

	for _, rel := range strings.Split(string(trackedOut), "\x00") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		abs := filepath.Join(repoPath, rel)
		if remErr := os.Remove(abs); remErr != nil && !os.IsNotExist(remErr) {
			return remErr
		}
	}

	return nil
}

func loadPreviewEntries(repoPath, fromTree, toTree string) ([]previewEntry, error) {
	predictor := git.NewConflictPredictor(repoPath)
	diffs, err := predictor.DiffTrees(fromTree, toTree)
	if err != nil {
		return nil, err
	}

	entries := make([]previewEntry, 0, len(diffs))
	for _, d := range diffs {
		patch, err := diffFileBetweenTrees(repoPath, fromTree, toTree, d.Path)
		if err != nil {
			patch = "Failed to render patch: " + err.Error()
		}
		entries = append(entries, previewEntry{Path: d.Path, Status: d.Status, Patch: patch})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func diffFileBetweenTrees(repoPath, fromTree, toTree, path string) (string, error) {
	cmd := exec.Command("git", "diff-tree", "-p", fromTree, toTree, "--", path)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func jumpStatePath(caryaPath string) string {
	return filepath.Join(caryaPath, "jump-state.json")
}

func writeJumpState(caryaPath string, state jumpState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(caryaPath, "jump-state-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, jumpStatePath(caryaPath))
}

func jumpInProgress(caryaPath string) bool {
	_, err := os.Stat(jumpStatePath(caryaPath))
	return err == nil
}

func readJumpState(caryaPath string) (jumpState, error) {
	data, err := os.ReadFile(jumpStatePath(caryaPath))
	if err != nil {
		if os.IsNotExist(err) {
			return jumpState{}, nil
		}
		return jumpState{}, err
	}

	var state jumpState
	if err := json.Unmarshal(data, &state); err != nil {
		return jumpState{}, err
	}

	return state, nil
}

func clearJumpState(caryaPath string) error {
	err := os.Remove(jumpStatePath(caryaPath))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func shortHash(hash string) string {
	if len(hash) < 12 {
		return hash
	}
	return hash[:12]
}

func init() {
	jumpCmd.Flags().BoolVarP(&forceJump, "force", "f", false, "Skip confirmation prompt")
	jumpCmd.Flags().StringVarP(&jumpUserFlag, "user", "u", "", "Target user (disambiguates reserved subcommand names)")
	jumpCmd.AddCommand(jumpLeaveCmd)
	jumpCmd.AddCommand(jumpViewCmd)
	rootCmd.AddCommand(jumpCmd)
}
