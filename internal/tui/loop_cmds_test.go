package tui

import (
	"sort"
	"testing"
)

func TestDetectCycles_NoCycle(t *testing.T) {
	// A → B → C (linear, no cycle)
	graph := map[string][]string{
		"1": {"2"},
		"2": {"3"},
	}
	result := detectCycles(graph)
	if len(result) != 0 {
		t.Errorf("expected no cycle nodes, got %v", result)
	}
}

func TestDetectCycles_SimpleCycle(t *testing.T) {
	// A → B, B → A
	graph := map[string][]string{
		"10": {"11"},
		"11": {"10"},
	}
	result := detectCycles(graph)
	if !result["10"] || !result["11"] {
		t.Errorf("expected both 10 and 11 in cycle, got %v", result)
	}
}

func TestDetectCycles_ThreeNodeCycle(t *testing.T) {
	// A → B → C → A
	graph := map[string][]string{
		"1": {"2"},
		"2": {"3"},
		"3": {"1"},
	}
	result := detectCycles(graph)
	if len(result) != 3 {
		t.Errorf("expected 3 nodes in cycle, got %d: %v", len(result), result)
	}
}

func TestDetectCycles_PartialCycle(t *testing.T) {
	// 1 → 2 → 3 → 2 (cycle: 2, 3), 1 is not in the cycle
	graph := map[string][]string{
		"1": {"2"},
		"2": {"3"},
		"3": {"2"},
	}
	result := detectCycles(graph)
	if !result["2"] || !result["3"] {
		t.Errorf("expected 2 and 3 in cycle, got %v", result)
	}
	// 1 is not part of the cycle itself
}

func TestDetectCycles_EmptyGraph(t *testing.T) {
	result := detectCycles(map[string][]string{})
	if len(result) != 0 {
		t.Errorf("expected empty result for empty graph, got %v", result)
	}
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	graph := map[string][]string{
		"5": {"5"},
	}
	result := detectCycles(graph)
	if !result["5"] {
		t.Errorf("expected 5 in cycle (self-loop), got %v", result)
	}
}

func TestDetectCycles_DisconnectedWithCycle(t *testing.T) {
	// Component 1: 1 → 2 (no cycle)
	// Component 2: 3 → 4 → 3 (cycle)
	graph := map[string][]string{
		"1": {"2"},
		"3": {"4"},
		"4": {"3"},
	}
	result := detectCycles(graph)

	// Sort keys for stable output
	var keys []string
	for k := range result {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if len(keys) != 2 || keys[0] != "3" || keys[1] != "4" {
		t.Errorf("expected cycle nodes [3 4], got %v", keys)
	}
}
