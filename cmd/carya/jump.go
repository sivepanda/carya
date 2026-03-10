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

	"carya/internal/git"
	"carya/internal/repository"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !repo.Exists() {
			fmt.Fprintf(os.Stderr, "Error: Not a Carya repository. Run 'carya init' first.\n")
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

		if err := checkoutTree(repo.RootPath(), treeHash); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking out tree: %v\n", err)
			os.Exit(1)
		}

		if err := writeJumpState(repo.CaryaPath(), jumpState{TargetUser: targetUser, StashHash: stashHash}); err != nil {
			fmt.Fprintf(os.Stderr, "Error recording jump state: %v\n", err)
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
		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !repo.Exists() {
			fmt.Fprintf(os.Stderr, "Error: Not a Carya repository. Run 'carya init' first.\n")
			os.Exit(1)
		}

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

		repo, err := repository.New()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !repo.Exists() {
			fmt.Fprintf(os.Stderr, "Error: Not a Carya repository. Run 'carya init' first.\n")
			os.Exit(1)
		}

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

		p := tea.NewProgram(newJumpPreviewModel(targetUser, entries), tea.WithAltScreen())
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
	list       viewport.Model
	diff       viewport.Model
	ready      bool
	width      int
	height     int
	listWidth  int
	diffWidth  int
}

func newJumpPreviewModel(targetUser string, entries []previewEntry) jumpPreviewModel {
	return jumpPreviewModel{targetUser: targetUser, entries: entries, width: 80, height: 24}
}

func (m jumpPreviewModel) Init() tea.Cmd { return nil }

func (m jumpPreviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.listWidth = msg.Width / 3
		if m.listWidth < 30 {
			m.listWidth = 30
		}
		m.diffWidth = msg.Width - m.listWidth - 1
		if m.diffWidth < 40 {
			m.diffWidth = 40
		}
		if !m.ready {
			m.list = viewport.New(m.listWidth-2, msg.Height-5)
			m.diff = viewport.New(m.diffWidth-2, msg.Height-5)
			m.ready = true
		} else {
			m.list.Width = m.listWidth - 2
			m.list.Height = msg.Height - 5
			m.diff.Width = m.diffWidth - 2
			m.diff.Height = msg.Height - 5
		}
		m.refreshContent()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.refreshContent()
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
				m.refreshContent()
			}
		case "ctrl+d":
			m.diff.ViewDown()
		case "ctrl+u":
			m.diff.ViewUp()
		}
	}

	return m, nil
}

func (m jumpPreviewModel) View() string {
	if !m.ready {
		return "Loading preview..."
	}

	title := lipgloss.NewStyle().Bold(true).Render("Jump Preview (read-only): " + m.targetUser)

	left := lipgloss.NewStyle().
		Width(m.listWidth).
		Height(m.height - 1).
		Border(lipgloss.RoundedBorder()).
		Render("Files\n" + m.list.View())

	right := lipgloss.NewStyle().
		Width(m.diffWidth).
		Height(m.height - 1).
		Border(lipgloss.RoundedBorder()).
		Render("Patch\n" + m.diff.View())

	help := lipgloss.NewStyle().Faint(true).Render("j/k or up/down navigate - ctrl+d/u scroll - q quit")

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, title, body, help)
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

	items := make([]string, 0, len(m.entries))
	for i, e := range m.entries {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		items = append(items, fmt.Sprintf("%s[%s] %s", prefix, e.Status, e.Path))
	}
	m.list.SetContent(strings.Join(items, "\n"))

	selected := m.entries[m.cursor]
	content := selected.Patch
	if strings.TrimSpace(content) == "" {
		content = "No patch content available."
	}
	m.diff.SetContent(content)
	m.diff.GotoTop()
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
	return os.WriteFile(jumpStatePath(caryaPath), data, 0644)
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
