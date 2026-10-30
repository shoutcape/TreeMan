package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/state"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

const warmStartsDocsURL = "https://github.com/shoutcape/TreeMan/blob/main/docs/reference/configuration.md#deps"

// offerWarmStarts is deliberately outside setup execution and summary rendering:
// accepting changes future setup, never reruns installation in the ready tree.
func offerWarmStarts(cmd *cobra.Command, project projectPaths, skipDeps bool) {
	if skipDeps || !canInteract(cmd) {
		return
	}
	// Discovery is bounded and read-only. Existence is a candidate signal, not
	// a promise of portability or speed; full copy validation happens on reuse.
	manifest, err := os.Lstat(filepath.Join(project.mainRoot, "package.json"))
	if err != nil || !manifest.Mode().IsRegular() {
		return
	}
	candidate, err := os.Lstat(filepath.Join(project.mainRoot, "node_modules"))
	if err != nil || !candidate.IsDir() {
		return
	}
	// Hooks or another command may have changed configuration since setup began.
	loaded := config.Load(project.mainRoot)
	if loaded.Warning != "" || (loaded.Config.Deps != nil && loaded.Config.Deps.Reuse != nil) {
		return
	}
	path := filepath.Join(project.mainRoot, config.ConfigFileName)
	if loaded.Path != "" && !samePath(loaded.Path, path) {
		return // Never edit inherited configuration shared with other repos.
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return
	} else if err != nil && !os.IsNotExist(err) {
		return
	}
	out := cmd.ErrOrStderr()
	render := commandRenderer(cmd)
	claimed, err := state.ClaimWarmStartPrompt(project.protected.CommonDir)
	if err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not record warm-start suggestion: %v", err)))
		return
	}
	if !claimed {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, render.Title("WARM STARTS"))
	fmt.Fprintln(out, "  Found node_modules in your main worktree.")
	fmt.Fprintln(out, "  TreeMan can copy it into future treebranches before installation.")
	fmt.Fprintln(out, "  Each copy stays independent. Faster setup is not guaranteed.")
	fmt.Fprintln(out)
	if sessionFor(cmd).errorOutput.Hyperlinks {
		fmt.Fprintln(out, "  "+render.Link("What are warm starts?", warmStartsDocsURL))
	} else {
		fmt.Fprintln(out, "  What are warm starts? "+warmStartsDocsURL)
	}
	fmt.Fprintf(out, "  Configure %s with [deps].reuse = [\"node_modules\"].\n", path)
	fmt.Fprintln(out, "  This suggestion is shown once per repository on this machine.")
	yes, err := confirmYN(cmd, "  Configure warm starts for this repo? [y/N] ", errConfirmationRequired)
	fmt.Fprintln(out)
	if err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("warm starts not configured: %v", err)))
		return
	}
	if !yes {
		fmt.Fprintln(out, "  Left configuration unchanged. You can enable warm starts manually later.")
		return
	}
	// The persistent exclusive claim ensures only one offer can reach this write.
	if err := config.EnableWarmStarts(path); err != nil {
		fmt.Fprintln(out, render.Status(ui.ToneWarning, "!", fmt.Sprintf("could not configure warm starts: %v; add [deps].reuse manually when ready", err)))
		return
	}
	fmt.Fprintln(out, render.Status(ui.ToneSuccess, "✓", "Warm starts configured for future treebranches. Current setup was not rerun."))
}
