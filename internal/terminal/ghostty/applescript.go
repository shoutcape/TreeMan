package ghostty

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/terminal"
)

// escapeAppleScript escapes backslashes and double-quotes for use inside
// AppleScript string literals.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// runAppleScript executes an AppleScript via osascript and returns its
// stdout (trimmed). On failure the combined output is included in the error.
func runAppleScript(script string) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// buildTitleHookCmd returns a shell one-liner that installs zsh precmd/preexec
// hooks to prefix the terminal title with the branch name.
// The result is: branch | <cwd> at prompt, branch | <command> during execution.
func buildTitleHookCmd(branch string) string {
	b := escapeAppleScript(branch)
	return fmt.Sprintf(
		`export TREEMAN_BRANCH=\"%s\"; `+
			`_tm_precmd() { print -Pn \"\\e]2;$TREEMAN_BRANCH | %%~\\a\" }; `+
			`_tm_preexec() { print -Pn \"\\e]2;$TREEMAN_BRANCH | $1\\a\" }; `+
			`add-zsh-hook precmd _tm_precmd; `+
			`add-zsh-hook preexec _tm_preexec; `+
			`clear`,
		b,
	)
}

// BuildOpenScript generates an AppleScript that opens a new Ghostty tab
// (or window if none exist), optionally splitting panes per layout.
// All panes start in the worktree directory. Each pane gets zsh hooks
// that prefix the terminal title with the branch name.
func BuildOpenScript(wt terminal.WorktreeInfo, layout *config.LayoutConfig) string {
	path := escapeAppleScript(wt.Path)
	titleHook := buildTitleHookCmd(wt.Branch)

	var b strings.Builder
	b.WriteString("tell application \"Ghostty\"\n")
	b.WriteString("    activate\n\n")
	b.WriteString("    set cfg to new surface configuration\n")
	b.WriteString(fmt.Sprintf("    set initial working directory of cfg to \"%s\"\n\n", path))
	b.WriteString("    if (count of windows) > 0 then\n")
	b.WriteString("        set t to new tab in front window with configuration cfg\n")
	b.WriteString("    else\n")
	b.WriteString("        set win to new window with configuration cfg\n")
	b.WriteString("        set t to selected tab of win\n")
	b.WriteString("    end if\n\n")
	b.WriteString("    set term to focused terminal of t\n")
	b.WriteString("    delay 0.5\n")
	b.WriteString(fmt.Sprintf("    input text \"%s\\n\" to term\n", titleHook))

	if layout != nil {
		for i, split := range layout.Splits {
			dir := escapeAppleScript(split.Direction)
			paneVar := fmt.Sprintf("pane%d", i+1)
			cfgVar := fmt.Sprintf("splitCfg%d", i+1)

			b.WriteString(fmt.Sprintf("\n    set %s to new surface configuration\n", cfgVar))
			b.WriteString(fmt.Sprintf("    set initial working directory of %s to \"%s\"\n", cfgVar, path))
			b.WriteString(fmt.Sprintf("    set %s to split term direction %s with configuration %s\n", paneVar, dir, cfgVar))
			b.WriteString("    delay 0.5\n")
			b.WriteString(fmt.Sprintf("    input text \"%s\\n\" to %s\n", titleHook, paneVar))

			if split.Command != "" {
				cmd := escapeAppleScript(split.Command)
				b.WriteString(fmt.Sprintf("    input text \"%s\\n\" to %s\n", cmd, paneVar))
			}
		}
	}

	b.WriteString("\n    focus term\nend tell")
	return b.String()
}

// BuildFocusScript generates an AppleScript that finds a terminal whose
// working directory matches the worktree path and focuses it.
// If the matching terminal is already focused in the frontmost app, it
// returns "found" without refocusing (avoiding unnecessary window flicker).
func BuildFocusScript(wt terminal.WorktreeInfo) string {
	path := escapeAppleScript(wt.Path)
	var b strings.Builder
	b.WriteString("tell application \"Ghostty\"\n")
	b.WriteString(fmt.Sprintf("    set matches to every terminal whose working directory is \"%s\"\n", path))
	b.WriteString("    if (count of matches) > 0 then\n")
	b.WriteString("        set matchedTerm to item 1 of matches\n")
	// Check if Ghostty is already frontmost and the matched terminal is
	// already focused. If so, skip the focus call to avoid flicker.
	b.WriteString("        set isFront to frontmost\n")
	b.WriteString("        if isFront then\n")
	b.WriteString("            try\n")
	b.WriteString("                set focusedTerm to focused terminal of (selected tab of front window)\n")
	b.WriteString("                if focusedTerm is matchedTerm then return \"found\"\n")
	b.WriteString("            end try\n")
	b.WriteString("        end if\n")
	b.WriteString("        focus matchedTerm\n")
	b.WriteString("        return \"found\"\n")
	b.WriteString("    end if\n")
	b.WriteString("    return \"not_found\"\n")
	b.WriteString("end tell")
	return b.String()
}

// BuildCloseScript generates an AppleScript that closes all terminals
// whose working directory starts with the worktree path.
func BuildCloseScript(wt terminal.WorktreeInfo) string {
	path := escapeAppleScript(wt.Path)
	var b strings.Builder
	b.WriteString("tell application \"Ghostty\"\n")
	b.WriteString(fmt.Sprintf("    set matches to every terminal whose working directory contains \"%s\"\n", path))
	b.WriteString("    repeat with t in matches\n")
	b.WriteString("        close t\n")
	b.WriteString("    end repeat\n")
	b.WriteString("end tell")
	return b.String()
}
