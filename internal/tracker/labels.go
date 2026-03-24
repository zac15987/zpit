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

// fetchExistingSet lists repo labels and returns them as a case-insensitive set plus the raw name list.
func fetchExistingSet(ctx context.Context, lm LabelManager, repo string) (set map[string]bool, names []string, err error) {
	names, err = lm.ListRepoLabels(ctx, repo)
	if err != nil {
		return nil, nil, fmt.Errorf("list labels: %w", err)
	}
	set = make(map[string]bool, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = true
	}
	return set, names, nil
}

// CheckLabels returns the subset of required labels that are missing from the repo (read-only).
// It also returns the full list of existing label names for caching.
func CheckLabels(ctx context.Context, lm LabelManager, repo string, required []LabelDef) (missing []LabelDef, allExisting []string, err error) {
	existingSet, allExisting, err := fetchExistingSet(ctx, lm, repo)
	if err != nil {
		return nil, nil, err
	}
	for _, req := range required {
		if !existingSet[strings.ToLower(req.Name)] {
			missing = append(missing, req)
		}
	}
	return missing, allExisting, nil
}

// EnsureLabels checks that all required labels exist in the repo and creates any that are missing.
// Returns the names of labels that were created.
func EnsureLabels(ctx context.Context, lm LabelManager, repo string, required []LabelDef) ([]string, error) {
	existingSet, _, err := fetchExistingSet(ctx, lm, repo)
	if err != nil {
		return nil, err
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
