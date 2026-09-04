package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPlanProducesDeterministicDependencyBatches(t *testing.T) {
	plan, err := buildPlan([]Container{
		testContainer("frontend", false, "application"),
		testContainer("database", false),
		testContainer("application", false, "database"),
		testContainer("cache", false),
	})
	require.NoError(t, err)

	assert.Equal(t, [][]string{{"cache", "database"}, {"application"}, {"frontend"}}, batchNames(plan.startup))
	assert.Equal(t, [][]string{{"frontend"}, {"application"}, {"cache", "database"}}, batchNames(plan.shutdown))
}

func TestBuildPlanRejectsDependencyCycle(t *testing.T) {
	_, err := buildPlan([]Container{
		testContainer("a", false, "b"),
		testContainer("b", false, "a"),
	})

	var cycle *cycleError
	require.ErrorAs(t, err, &cycle)
	assert.Equal(t, []string{"a", "b"}, cycle.containers)
}

func TestBuildPlanFiltersUnmanagedAndControllerDisabledContainers(t *testing.T) {
	unmanaged := testContainer("unmanaged", false)
	unmanaged.Labels[managedLabel] = "false"
	disabled := testContainer("disabled", false)
	disabled.Labels[labelController] = "false"
	configured := testContainer("configured", false)
	configured.Labels[labelDependsOnDelay] = "7"

	plan, err := buildPlan([]Container{unmanaged, disabled, configured})
	require.NoError(t, err)
	require.Len(t, plan.nodes, 1)
	assert.Equal(t, "configured", plan.nodes[0].container.Name)
	assert.Equal(t, 7*time.Second, plan.nodes[0].delay)
}

func batchNames(batches [][]*node) [][]string {
	names := make([][]string, len(batches))
	for i, batch := range batches {
		for _, current := range batch {
			names[i] = append(names[i], current.container.Name)
		}
	}
	return names
}
