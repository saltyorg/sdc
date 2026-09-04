package lifecycle

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	labelManaged         = "com.github.saltbox.saltbox_managed"
	labelController      = "com.github.saltbox.saltbox_controller"
	labelDependsOn       = "com.github.saltbox.depends_on"
	labelDependsOnDelay  = "com.github.saltbox.depends_on.delay"
	labelDependsOnHealth = "com.github.saltbox.depends_on.healthchecks"
)

type node struct {
	container  Container
	parents    []*node
	children   []*node
	missing    []string
	delay      time.Duration
	waitHealth bool
}

type executionPlan struct {
	nodes    []*node
	startup  [][]*node
	shutdown [][]*node
}

type cycleError struct {
	containers []string
}

func (e *cycleError) Error() string {
	return fmt.Sprintf("dependency cycle detected: %s", strings.Join(e.containers, ", "))
}

func buildPlan(containers []Container) (*executionPlan, error) {
	nodesByName := make(map[string]*node)
	for _, container := range containers {
		if !managed(container.Labels) {
			continue
		}
		name := strings.TrimSpace(container.Name)
		if name == "" {
			name = container.ID
		}
		if name == "" {
			continue
		}
		if _, exists := nodesByName[name]; exists {
			return nil, fmt.Errorf("duplicate container name %q", name)
		}
		container.Name = name
		nodesByName[name] = &node{
			container:  container,
			delay:      parseDelay(container.Labels[labelDependsOnDelay]),
			waitHealth: strings.EqualFold(container.Labels[labelDependsOnHealth], "true"),
		}
	}

	names := make([]string, 0, len(nodesByName))
	for name := range nodesByName {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		current := nodesByName[name]
		for _, dependency := range parseDependencies(current.container.Labels[labelDependsOn]) {
			parent, exists := nodesByName[dependency]
			if !exists {
				current.missing = append(current.missing, dependency)
				continue
			}
			current.parents = append(current.parents, parent)
			parent.children = append(parent.children, current)
		}
		sort.Strings(current.missing)
	}

	indegree := make(map[string]int, len(nodesByName))
	ready := make([]*node, 0)
	for _, name := range names {
		current := nodesByName[name]
		indegree[name] = len(current.parents)
		if len(current.parents) == 0 {
			ready = append(ready, current)
		}
	}

	var batches [][]*node
	visited := 0
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return ready[i].container.Name < ready[j].container.Name })
		batch := append([]*node(nil), ready...)
		batches = append(batches, batch)
		ready = nil
		for _, current := range batch {
			visited++
			for _, child := range current.children {
				indegree[child.container.Name]--
				if indegree[child.container.Name] == 0 {
					ready = append(ready, child)
				}
			}
		}
	}

	if visited != len(nodesByName) {
		var cycle []string
		for _, name := range names {
			if indegree[name] > 0 {
				cycle = append(cycle, name)
			}
		}
		return nil, &cycleError{containers: cycle}
	}

	shutdown := make([][]*node, len(batches))
	for i, batch := range batches {
		shutdown[len(batches)-1-i] = batch
	}
	nodes := make([]*node, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, nodesByName[name])
	}
	return &executionPlan{nodes: nodes, startup: batches, shutdown: shutdown}, nil
}

func managed(labels map[string]string) bool {
	return strings.EqualFold(labels[labelManaged], "true") && !strings.EqualFold(labels[labelController], "false")
}

func parseDependencies(value string) []string {
	seen := make(map[string]struct{})
	var dependencies []string
	for dependency := range strings.SplitSeq(value, ",") {
		dependency = strings.TrimSpace(dependency)
		if dependency == "" {
			continue
		}
		if _, exists := seen[dependency]; exists {
			continue
		}
		seen[dependency] = struct{}{}
		dependencies = append(dependencies, dependency)
	}
	sort.Strings(dependencies)
	return dependencies
}

func parseDelay(value string) time.Duration {
	seconds, err := strconv.ParseInt(value, 10, 32)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
