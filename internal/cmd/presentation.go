package cmd

import (
	"context"

	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

var terminalCapabilities = terminal.Detect

type terminalSession struct {
	errorOutput terminal.Capabilities
	standardOut terminal.Capabilities
}

type terminalSessionKey struct{}

// commandContext is the context a command's work runs under. cobra leaves it
// nil until the command is executed, so a command built directly — by a test,
// or by a helper that never goes through Execute — still needs a parent.
func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// sessionFor caches stream capabilities on the Cobra command. Rendering and
// interaction share this decision rather than probing terminal state ad hoc.
func sessionFor(cmd *cobra.Command) terminalSession {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if session, ok := ctx.Value(terminalSessionKey{}).(terminalSession); ok {
		return session
	}
	session := terminalSession{
		errorOutput: terminalCapabilities(cmd.InOrStdin(), cmd.ErrOrStderr()),
		standardOut: terminalCapabilities(cmd.InOrStdin(), cmd.OutOrStdout()),
	}
	cmd.SetContext(context.WithValue(ctx, terminalSessionKey{}, session))
	return session
}

// commandRenderer binds human output to the command's configured error stream.
func commandRenderer(cmd *cobra.Command) ui.Renderer {
	err := cmd.ErrOrStderr()
	return ui.NewRenderer(err, sessionFor(cmd).errorOutput)
}

// outputRenderer binds report output to the command's configured output stream.
func outputRenderer(cmd *cobra.Command) ui.Renderer {
	out := cmd.OutOrStdout()
	return ui.NewRenderer(out, sessionFor(cmd).standardOut)
}

func canInteract(cmd *cobra.Command) bool {
	return sessionFor(cmd).errorOutput.Interactive
}
