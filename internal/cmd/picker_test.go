package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shoutcape/treeman/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFZFHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FZF_HELPER_PROCESS") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if marker := os.Getenv("FZF_EOF_MARKER"); marker != "" {
		_ = os.WriteFile(marker, nil, 0o600)
	}
	time.Sleep(30 * time.Second)
}

func TestRunStreamingPickerStopsFZFWhenParentIsCancelledAfterRowsFinish(t *testing.T) {
	t.Setenv("GO_WANT_FZF_HELPER_PROCESS", "1")
	directory := t.TempDir()
	marker := filepath.Join(directory, "eof")
	t.Setenv("FZF_EOF_MARKER", marker)
	helper := filepath.Join(directory, "fzf")
	script := "#!/bin/sh\nexec \"" + os.Args[0] + "\" -test.run=^TestFZFHelperProcess$\n"
	require.NoError(t, os.WriteFile(helper, []byte(script), 0o700))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	done := make(chan error, 1)
	go func() {
		_, _, err := runStreamingPicker(cmd, pickerRequest{header: "HEADER"}, func(_ context.Context, emit func(string) error) error {
			if err := emit("one"); err != nil {
				return err
			}
			return emit("two")
		})
		done <- err
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("parent cancellation left fzf running")
	}
}

func TestStreamPickerRowsWritesHeaderThenIndexedRows(t *testing.T) {
	out := &bytes.Buffer{}

	count, err := streamPickerRows(context.Background(), out, "HEADER", func(_ context.Context, emit func(string) error) error {
		if err := emit("first"); err != nil {
			return err
		}
		return emit("second")
	})

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, "HEADER\nfirst\t0\nsecond\t1\n", out.String())
}

func TestStreamPickerRowsReportsAnEmptyProducer(t *testing.T) {
	out := &bytes.Buffer{}

	count, err := streamPickerRows(context.Background(), out, "HEADER", func(context.Context, func(string) error) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "HEADER\n", out.String())
}

// The picker is only faster than a buffered payload if each row reaches fzf
// before the next one is produced.
func TestStreamPickerRowsDeliversEachRowBeforeProducingTheNext(t *testing.T) {
	out := &bytes.Buffer{}
	var seen []string

	_, err := streamPickerRows(context.Background(), out, "HEADER", func(_ context.Context, emit func(string) error) error {
		seen = append(seen, out.String())
		if err := emit("first"); err != nil {
			return err
		}
		seen = append(seen, out.String())
		return emit("second")
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"HEADER\n", "HEADER\nfirst\t0\n"}, seen)
}

func TestStreamPickerRowsPropagatesProducerFailure(t *testing.T) {
	out := &bytes.Buffer{}

	_, err := streamPickerRows(context.Background(), out, "HEADER", func(_ context.Context, emit func(string) error) error {
		require.NoError(t, emit("first"))
		return assert.AnError
	})

	assert.ErrorIs(t, err, assert.AnError)
}

// fzf closes its stdin as soon as the user selects, which must read as "the
// consumer is done", not as a failure.
func TestStreamPickerRowsTreatsAClosedConsumerAsDone(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { writer.Close() })
	require.NoError(t, reader.Close())

	count, err := streamPickerRows(context.Background(), writer, "HEADER", func(_ context.Context, emit func(string) error) error {
		for range 100000 {
			if err := emit(strings.Repeat("row", 100)); err != nil {
				return err
			}
		}
		return nil
	})

	require.NoError(t, err)
	assert.Less(t, count, 100000)
}

// A picker whose list is ready up front can let fzf settle it, because there
// is no wait to shorten.
func TestPickerArgsLetFzfSettleACompleteList(t *testing.T) {
	args := pickerArgs(false, " worktrees ", "switch > ")

	assert.Contains(t, args, "--select-1")
	assert.Contains(t, args, "--exit-0")
}

// fzf paints nothing while --select-1 or --exit-0 is set, so a streamed picker
// without a query must not use them: the terminal would stay blank until the
// first row arrived.
func TestStreamingPickerArgsDropTheFlagsThatDelayThefirstPaint(t *testing.T) {
	args := streamingPickerArgs(false, pickerRequest{label: " branches ", prompt: "branch > "})

	assert.NotContains(t, args, "--select-1")
	assert.NotContains(t, args, "--exit-0")
	assert.NotContains(t, args, "--query")
	assert.Contains(t, args, "--header-lines=1")
	assert.Contains(t, args, "--prompt=branch > ")
	assert.False(t, pickerSettlesItself(pickerRequest{}))
}

// With a query only fzf knows how many rows match it, so it keeps the flags
// and the first paint waits.
func TestStreamingPickerArgsKeepTheFlagsForAQuery(t *testing.T) {
	request := pickerRequest{label: " branches ", prompt: "branch > ", query: "feature"}
	args := streamingPickerArgs(false, request)

	assert.Contains(t, args, "--select-1")
	assert.Contains(t, args, "--exit-0")
	assert.Contains(t, args, "--query")
	assert.Contains(t, args, "feature")
	assert.True(t, pickerSettlesItself(request))
}

// The picker resolves how fzf exited into a row index, so each way it can end
// has to map onto the right outcome.
func TestPickerSelectionMapsHowFzfExited(t *testing.T) {
	index, count, err := pickerSelection(nil, "branch\t2\n", 3)
	require.NoError(t, err)
	assert.Equal(t, 2, index)
	assert.Equal(t, 3, count)

	_, _, err = pickerSelection(nil, "", 3)
	assert.ErrorIs(t, err, errPickerCancelled, "an empty selection is an abort")

	_, _, err = pickerSelection(nil, "branch\t9\n", 3)
	assert.ErrorContains(t, err, "could not map the fzf selection")

	_, _, err = pickerSelection(assert.AnError, "", 0)
	assert.ErrorIs(t, err, errPickerCancelled, "nothing was ever offered to pick")

	_, _, err = pickerSelection(assert.AnError, "", 3)
	assert.ErrorContains(t, err, "fzf failed while selecting")
}

// The streamed picker numbers rows after a header line fzf is told to hold
// out of the list. This runs the real fzf over a streamed payload to hold it
// to that: the header must never come back as a selection, the index must
// never be displayed or matched, and rows that look the same must still carry
// the index of the row that produced them.
func TestStreamedRowsKeepTheirOwnIndexBehindTheHeaderLine(t *testing.T) {
	requireRealFZF(t)

	payload := &bytes.Buffer{}
	count, err := streamPickerRows(context.Background(), payload, "HEADER", func(_ context.Context, emit func(string) error) error {
		for _, display := range []string{"dup", "dup", "other"} {
			if err := emit(display); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, count)

	filter := func(query string) []string {
		t.Helper()
		args := append(streamingPickerArgs(false, pickerRequest{label: " rows ", prompt: "row > "}), "--filter="+query)
		fzf := exec.Command("fzf", args...)
		fzf.Stdin = bytes.NewReader(payload.Bytes())
		out, _ := fzf.Output()
		if strings.TrimSpace(string(out)) == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	}

	assert.Empty(t, filter("HEADER"), "the header is not a row the user can pick")
	assert.Empty(t, filter("2"), "the row index is neither displayed nor matched")

	duplicates := filter("dup")
	require.Len(t, duplicates, 2)
	assert.Equal(t, 0, pickerSelectionIndex(duplicates[0], count))
	assert.Equal(t, 1, pickerSelectionIndex(duplicates[1], count))
}

// requireRealFZF skips unless the fzf on PATH answers a --filter query the way
// fzf does.
//
// A test that shells out to whatever binary is named fzf is only testing what
// that binary happens to do. Test environments do put a stand-in there — the
// smoke suite installs one that ignores its arguments and always returns the
// same row — and a stand-in would fail the assertions below for a reason that
// says nothing about the picker.
func requireRealFZF(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("fzf"); err != nil {
		t.Skip("fzf is not installed")
	}
	probe := exec.Command("fzf", "--filter=second")
	probe.Stdin = strings.NewReader("first\nsecond\n")
	out, err := probe.Output()
	if err != nil || strings.TrimSpace(string(out)) != "second" {
		t.Skip("the fzf on PATH does not filter like fzf")
	}
}

// Two worktrees can render the same picker row: the display shows only the
// last two path components and the branch, so nested directories that repeat
// those names look identical. The row a picker returns therefore has to be
// resolved by the index the row carries, never by the text the user saw.
func TestSwitchResolvesIdenticalDisplayRowsToTheirOwnWorktree(t *testing.T) {
	repo, first, second := repoWithIdenticalWorktreeRows(t)
	chdirForTest(t, repo)

	// Row 1 is the repository's own worktree; the two identical rows follow it.
	assert.Equal(t, first, switchWithPickedRow(t, 2), "the first duplicate row must resolve to the first worktree")
	assert.Equal(t, second, switchWithPickedRow(t, 3), "the second duplicate row must resolve to the second worktree")
}

// switchWithPickedRow runs switch against a picker that always answers with
// the given one-based row of the list it was handed, and returns the worktree
// path switch printed for the shell wrapper.
func switchWithPickedRow(t *testing.T, row int) string {
	t.Helper()
	stubFZFPicksRow(t, row)

	var stdout, stderr bytes.Buffer
	cmd := interactiveCommand(&stdout, &stderr)
	require.NoError(t, runSwitch(cmd, ""))

	return strings.TrimSpace(stdout.String())
}

// repoWithIdenticalWorktreeRows builds a repository whose two added worktrees
// render the same picker row: same last two path components, and no branch
// name to tell them apart because both are detached.
func repoWithIdenticalWorktreeRows(t *testing.T) (repo, first, second string) {
	t.Helper()
	parent := t.TempDir()
	repo = filepath.Join(parent, "repo")
	require.NoError(t, os.Mkdir(repo, 0o755))
	gitTest(t, repo, "init", "-b", "main")
	gitTest(t, repo, "config", "user.name", "TreeMan Test")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644))
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "commit", "-m", "initial")

	first = filepath.Join(parent, "one", "nested", "work")
	second = filepath.Join(parent, "two", "nested", "work")
	gitTest(t, repo, "worktree", "add", "--detach", first)
	gitTest(t, repo, "worktree", "add", "--detach", second)

	render := commandRenderer(interactiveCommand(&bytes.Buffer{}, &bytes.Buffer{}))
	require.Equal(t, render.WorktreeRow(first, ""), render.WorktreeRow(second, ""),
		"the test needs two worktrees the picker cannot tell apart by display text")
	return repo, first, second
}

// stubFZFPicksRow puts an fzf on PATH that returns the given one-based line of
// its input unchanged, which is what fzf prints for a selected row.
func stubFZFPicksRow(t *testing.T, row int) {
	t.Helper()
	directory := t.TempDir()
	stub := filepath.Join(directory, "fzf")
	script := "#!/bin/sh\nsed -n '" + strconv.Itoa(row) + "p'\n"
	require.NoError(t, os.WriteFile(stub, []byte(script), 0o700))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// interactiveCommand is a command whose streams report a terminal, so the
// picker paths run instead of refusing to interact.
func interactiveCommand(stdout, stderr *bytes.Buffer) *cobra.Command {
	cmd := commandWithOutput(stdout, stderr)
	interactive := terminal.Capabilities{InputTTY: true, OutputTTY: true, Interactive: true, Width: 120}
	cmd.SetContext(context.WithValue(context.Background(), terminalSessionKey{}, terminalSession{
		errorOutput: interactive,
		standardOut: interactive,
	}))
	return cmd
}
