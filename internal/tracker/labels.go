package tracker

import (
	"context"
	"fmt"
	"strings"
)

// LabelManager is implemented by tracker clients that can list and create labels.
type LabelManager interface {
	ListRepoLabels(ctx context.Context, repo string) ([]string, error)
	CreateLabel(ctx context.Context, repo string, label LabelDef) error
}

// EnsureLabels checks that all required labels exist in the repo and creates any that are missing.
// Returns the names of labels that were created.
func EnsureLabels(ctx context.Context, lm LabelManager, repo string, required []LabelDef) ([]string, error) {
	existing, err := lm.ListRepoLabels(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}

	existingSet := make(map[string]bool, len(existing))
	for _, name := range existing {
		existingSet[strings.ToLower(name)] = true
	}

	var created []string
	for _, req := range required {
		if existingSet[strings.ToLower(req.Name)] {
			continue
		}
		if err := lm.CreateLabel(ctx, repo, req); err != nil {
			return created, fmt.Errorf("create label %q: %w", req.Name, err)
		}
		created = append(created, req.Name)
	}
	return created, nil
}
