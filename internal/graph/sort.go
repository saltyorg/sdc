package graph

import (
	"fmt"
)

// TopologicalSort performs a topological sort on the dependency graph
// Returns containers in startup order (dependencies first)
func (g *Graph) TopologicalSort() (*SortedContainers, error) {
	// Validate graph first
	if err := g.Validate(); err != nil {
		return nil, err
	}

	// Reset visited flags
	for _, node := range g.Nodes {
		node.visited = false
		node.sortIndex = -1
	}

	var sorted []*Node
	index := 0

	// DFS-based topological sort
	var visit func(*Node) error
	visit = func(node *Node) error {
		if node.visited {
			return nil
		}

		node.visited = true

		// Visit all parents first (dependencies)
		for _, parent := range node.Parents {
			if err := visit(parent); err != nil {
				return err
			}
		}

		// Skip placeholder nodes in the final output
		if !node.IsPlaceholder {
			node.sortIndex = index
			sorted = append(sorted, node)
			index++
		}

		return nil
	}

	// Visit all nodes
	for _, node := range g.Nodes {
		if !node.visited && !node.IsPlaceholder {
			if err := visit(node); err != nil {
				return nil, err
			}
		}
	}

	if len(sorted) == 0 {
		return nil, fmt.Errorf("no containers to sort")
	}

	// Create shutdown order (reverse of startup order)
	shutdown := make([]*Node, len(sorted))
	for i, node := range sorted {
		shutdown[len(sorted)-1-i] = node
	}

	return &SortedContainers{
		StartupOrder:  sorted,
		ShutdownOrder: shutdown,
	}, nil
}

// GetStartupBatches groups containers into batches that can be started in parallel
// Containers in the same batch have no dependencies on each other
func (g *Graph) GetStartupBatches() ([][]*Node, error) {
	sorted, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	// Calculate the depth of each node (longest path from roots)
	depths := make(map[string]int)

	var calculateDepth func(*Node) int
	calculateDepth = func(node *Node) int {
		if depth, exists := depths[node.Name]; exists {
			return depth
		}

		if !node.HasParents() {
			depths[node.Name] = 0
			return 0
		}

		maxParentDepth := -1
		for _, parent := range node.Parents {
			if parent.IsPlaceholder {
				continue
			}
			parentDepth := calculateDepth(parent)
			if parentDepth > maxParentDepth {
				maxParentDepth = parentDepth
			}
		}

		depth := maxParentDepth + 1
		depths[node.Name] = depth
		return depth
	}

	// Calculate depths for all nodes
	maxDepth := 0
	for _, node := range sorted.StartupOrder {
		depth := calculateDepth(node)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	// Group nodes by depth into batches
	batches := make([][]*Node, maxDepth+1)
	for _, node := range sorted.StartupOrder {
		depth := depths[node.Name]
		batches[depth] = append(batches[depth], node)
	}

	return batches, nil
}

// GetShutdownBatches groups containers into batches for parallel shutdown
// This is the reverse of startup batches
func (g *Graph) GetShutdownBatches() ([][]*Node, error) {
	startupBatches, err := g.GetStartupBatches()
	if err != nil {
		return nil, err
	}

	// Reverse the batches
	shutdownBatches := make([][]*Node, len(startupBatches))
	for i, batch := range startupBatches {
		shutdownBatches[len(startupBatches)-1-i] = batch
	}

	return shutdownBatches, nil
}

// FilterByState returns nodes filtered by running state
func FilterByState(nodes []*Node, running bool) []*Node {
	var filtered []*Node
	for _, node := range nodes {
		if node.IsRunning == running {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

// FilterByNames returns nodes filtered by a list of names to exclude
func FilterByNames(nodes []*Node, excludeNames map[string]bool) []*Node {
	var filtered []*Node
	for _, node := range nodes {
		if !excludeNames[node.Name] {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

// GetNodeNames extracts container names from a list of nodes
func GetNodeNames(nodes []*Node) []string {
	names := make([]string, len(nodes))
	for i, node := range nodes {
		names[i] = node.Name
	}
	return names
}
