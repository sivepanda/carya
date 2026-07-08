package compose

import (
	"carya/internal/chunk"
	"carya/internal/patch"
	"fmt"
	"sort"
	"strings"
)

const defaultRecentChunkLimit = 100

//gloop glopr lopr
//plor glor troll

type ChunkStore interface {
	GetRecentChunks(limit int) ([]chunk.Chunk, error)
	UpdateChunkFeatureLabel(ids []chunk.ChunkID, label string) error
	ClearChunkFeatureLabel(ids []chunk.ChunkID) error
}

type Composer struct {
	store         ChunkStore
	repoPath      string
	chunks        []chunk.Chunk
	selected      map[int]bool
	pendingLabels map[chunk.ChunkID]string
	dirtyLabels   bool
}

type CommitResult struct {
	Output        string
	Warning       *Warning
	ApplyConflict *ApplyConflict
}

type Warning struct {
	Warnings  []string
	Patch     string
	CommitMsg string
}

type ApplyConflict struct {
	TotalSelected int
	Applicable    map[int]bool
	Skipped       []string
	CommitMsg     string
}

func New(store ChunkStore, repoPath string) (*Composer, error) {
	chunks, err := store.GetRecentChunks(defaultRecentChunkLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}

	return &Composer{
		store:         store,
		repoPath:      repoPath,
		chunks:        chunks,
		selected:      make(map[int]bool),
		pendingLabels: make(map[chunk.ChunkID]string),
	}, nil
}

func (c *Composer) Chunks() []chunk.Chunk {
	return c.chunks
}

func (c *Composer) IsSelected(index int) bool {
	return c.selected[index]
}

func (c *Composer) SetSelected(index int, selected bool) {
	if index < 0 || index >= len(c.chunks) {
		return
	}

	if selected {
		c.selected[index] = true
		return
	}

	delete(c.selected, index)
}

func (c *Composer) ToggleSelection(index int) {
	if index < 0 || index >= len(c.chunks) {
		return
	}

	c.SetSelected(index, !c.IsSelected(index))
}

func (c *Composer) SelectedChunkCount() int {
	count := 0
	for _, selected := range c.selected {
		if selected {
			count++
		}
	}
	return count
}

func (c *Composer) SelectedChunkIndices() []int {
	indices := make([]int, 0)
	for i, selected := range c.selected {
		if selected {
			indices = append(indices, i)
		}
	}
	sort.Ints(indices)
	return indices
}

func (c *Composer) ChunkIndicesForLabel(label string) []int {
	normalized := strings.TrimSpace(label)
	indices := make([]int, 0)
	for i, ch := range c.chunks {
		if strings.TrimSpace(ch.FeatureLabel) == normalized {
			indices = append(indices, i)
		}
	}
	return indices
}

func (c *Composer) AssignLabelToSelected(label string) int {
	label = strings.TrimSpace(label)
	if label == "" {
		return 0
	}

	updated := 0
	for _, idx := range c.SelectedChunkIndices() {
		if idx < 0 || idx >= len(c.chunks) {
			continue
		}
		c.chunks[idx].FeatureLabel = label
		c.pendingLabels[c.chunks[idx].ID] = label
		updated++
	}

	if updated > 0 {
		c.dirtyLabels = true
	}

	return updated
}

func (c *Composer) ClearLabelForSelected() int {
	updated := 0
	for _, idx := range c.SelectedChunkIndices() {
		if idx < 0 || idx >= len(c.chunks) {
			continue
		}
		if c.chunks[idx].FeatureLabel == "" {
			continue
		}
		c.chunks[idx].FeatureLabel = ""
		c.pendingLabels[c.chunks[idx].ID] = ""
		updated++
	}

	if updated > 0 {
		c.dirtyLabels = true
	}

	return updated
}

func (c *Composer) DirtyLabels() bool {
	return c.dirtyLabels
}

func (c *Composer) PersistPendingLabels() (int, error) {
	if len(c.pendingLabels) == 0 {
		c.dirtyLabels = false
		return 0, nil
	}

	idsByLabel := make(map[string][]chunk.ChunkID)
	for id, label := range c.pendingLabels {
		idsByLabel[label] = append(idsByLabel[label], id)
	}

	for label, ids := range idsByLabel {
		if strings.TrimSpace(label) == "" {
			if err := c.store.ClearChunkFeatureLabel(ids); err != nil {
				return 0, err
			}
			continue
		}

		if err := c.store.UpdateChunkFeatureLabel(ids, label); err != nil {
			return 0, err
		}
	}

	saved := len(c.pendingLabels)
	c.pendingLabels = make(map[chunk.ChunkID]string)
	c.dirtyLabels = false
	return saved, nil
}

func (c *Composer) CreateCommit(commitMsg string) (CommitResult, error) {
	if _, err := c.PersistPendingLabels(); err != nil {
		return CommitResult{}, fmt.Errorf("failed to save feature labels: %w", err)
	}

	result := patch.CreateFromChunks(c.chunks, c.selected)
	if len(result.Patch) == 0 {
		return CommitResult{}, fmt.Errorf("no valid patches to apply")
	}

	if len(result.Warnings) > 0 {
		return CommitResult{
			Warning: &Warning{
				Warnings:  result.Warnings,
				Patch:     result.Patch,
				CommitMsg: commitMsg,
			},
		}, nil
	}

	if err := patch.CheckApply(result.Patch); err != nil {
		applicable, skipped := c.classifySelectedChunks()

		applicableCount := 0
		for _, selected := range applicable {
			if selected {
				applicableCount++
			}
		}

		if applicableCount > 0 && applicableCount < c.SelectedChunkCount() {
			return CommitResult{
				ApplyConflict: &ApplyConflict{
					TotalSelected: c.SelectedChunkCount(),
					Applicable:    applicable,
					Skipped:       skipped,
					CommitMsg:     commitMsg,
				},
			}, nil
		}

		return CommitResult{}, err
	}

	output, err := c.ApplyPatchAndCommit(result.Patch, commitMsg)
	if err != nil {
		return CommitResult{}, err
	}

	return CommitResult{Output: output}, nil
}

func (c *Composer) ApplyPatchAndCommit(patchText, commitMsg string) (string, error) {
	if err := patch.Apply(patchText); err != nil {
		return "", err
	}

	output, err := patch.Commit(commitMsg)
	if err != nil {
		return "", err
	}

	return output, nil
}

func (c *Composer) ApplyApplicableCommit(applicable map[int]bool, commitMsg string) (string, error) {
	result := patch.CreateFromChunks(c.chunks, applicable)
	if len(result.Patch) == 0 {
		return "", fmt.Errorf("no applicable patches to apply")
	}

	if err := patch.CheckApply(result.Patch); err != nil {
		return "", err
	}

	return c.ApplyPatchAndCommit(result.Patch, commitMsg)
}

func (c *Composer) classifySelectedChunks() (map[int]bool, []string) {
	applicable := make(map[int]bool)
	var skipped []string

	for i, ch := range c.chunks {
		if selected, ok := c.selected[i]; !ok || !selected {
			continue
		}

		decision, reason := chunk.DecideRetention(c.repoPath, ch)
		if decision == chunk.DecisionKeep {
			applicable[i] = true
			continue
		}

		skipped = append(skipped, fmt.Sprintf("%s (%s)", ch.FilePath, reason))
	}

	return applicable, skipped
}
