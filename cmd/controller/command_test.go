package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/saltyorg/sdc/internal/config"
	"github.com/saltyorg/sdc/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCommandLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(false)
	require.NoError(t, err)
	return log
}

func TestRootCommandBuildsFreshTreesAndPassesContext(t *testing.T) {
	type contextKey string
	type serverCall struct {
		ctx context.Context
		cfg config.ServerConfig
	}
	var calls []serverCall
	runners := commandRunners{
		server: func(ctx context.Context, cfg config.ServerConfig) error {
			calls = append(calls, serverCall{ctx: ctx, cfg: cfg})
			return nil
		},
		helper: func(context.Context, config.HelperConfig) error { return nil },
	}

	const key contextKey = "test-key"
	firstContext := context.WithValue(t.Context(), key, "first")
	first := newRootCommand(runners)
	first.SetArgs([]string{"server", "--host", "0.0.0.0", "--port", "4444"})
	require.NoError(t, first.ExecuteContext(firstContext))

	second := newRootCommand(runners)
	second.SetArgs([]string{"server"})
	require.NoError(t, second.ExecuteContext(t.Context()))

	require.Len(t, calls, 2)
	assert.Equal(t, "0.0.0.0", calls[0].cfg.Host)
	assert.Equal(t, 4444, calls[0].cfg.Port)
	assert.Equal(t, "first", calls[0].ctx.Value(key))
	assert.Equal(t, "127.0.0.1", calls[1].cfg.Host)
	assert.Equal(t, 3377, calls[1].cfg.Port)
}

func TestRootCommandSilencesFrameworkErrorOutput(t *testing.T) {
	root := newRootCommand(commandRunners{
		server: func(context.Context, config.ServerConfig) error { return nil },
		helper: func(context.Context, config.HelperConfig) error { return nil },
	})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"not-a-command"})

	err := root.ExecuteContext(t.Context())

	assert.Error(t, err)
	assert.Empty(t, output.String())
}
