// Package cli implements the epm-cli command tree.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/droidnik/epm-cli/internal/apperr"
	"github.com/droidnik/epm-cli/internal/client"
	"github.com/droidnik/epm-cli/internal/config"
	"github.com/droidnik/epm-cli/internal/ojson"
	"github.com/droidnik/epm-cli/internal/output"
)

// Version is injected at build time (-ldflags "-X github.com/droidnik/epm-cli/internal/cli.Version=…").
var Version = "dev"

// App is the per-invocation state shared by all commands.
type App struct {
	Out, Err io.Writer
	In       io.Reader
	InTTY    bool
	OutTTY   bool
	ErrTTY   bool

	// global flags
	Profile, Output, Field, Columns, OutFile string
	Wide, DryRun, Yes, NoInput, NoColor      bool
	Quiet, Verbose                           bool

	cfg    *config.Config
	client *client.Client
	stdin  *bufio.Reader
}

// NewApp returns an App wired to the process's stdio.
func NewApp() *App {
	return &App{
		Out: os.Stdout, Err: os.Stderr, In: os.Stdin,
		InTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		OutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		ErrTTY: term.IsTerminal(int(os.Stderr.Fd())),
	}
}

func (a *App) format() string {
	if a.Output != "" {
		return a.Output
	}
	if f := os.Getenv("EPM_OUTPUT"); f != "" {
		return f
	}
	return "table"
}

func (a *App) machineOutput() bool {
	f := a.format()
	return f == "json" || f == "ndjson" || f == "yaml"
}

func (a *App) interactive() bool { return !a.NoInput && a.InTTY }

func (a *App) colorErr() bool {
	return a.ErrTTY && !a.NoColor && os.Getenv("NO_COLOR") == ""
}

func (a *App) Config() (*config.Config, error) {
	if a.cfg == nil {
		c, err := config.Load()
		if err != nil {
			return nil, err
		}
		a.cfg = c
	}
	return a.cfg, nil
}

func (a *App) ResolveProfile() (*config.Profile, error) {
	cfg, err := a.Config()
	if err != nil {
		return nil, err
	}
	return cfg.Resolve(a.Profile)
}

// Client returns the API client for the active profile (created lazily).
func (a *App) Client() (*client.Client, error) {
	if a.client == nil {
		p, err := a.ResolveProfile()
		if err != nil {
			return nil, err
		}
		client.Version = Version
		a.client = client.New(p, a.Verbose, a.Log)
	}
	return a.client, nil
}

// resetClient drops the cached client (after the profile changed).
func (a *App) resetClient() { a.client = nil; a.cfg = nil }

func (a *App) Log(msg string) { fmt.Fprintln(a.Err, msg) }

// Hint prints advisory output to stderr unless --quiet / EPM_CLI_NO_HINTS.
func (a *App) Hint(msg string) {
	if !a.Quiet && os.Getenv("EPM_CLI_NO_HINTS") == "" {
		fmt.Fprintln(a.Err, msg)
	}
}

func (a *App) Success(msg string) {
	if a.Quiet {
		return
	}
	if a.colorErr() {
		fmt.Fprintf(a.Err, "\033[32m✓\033[0m %s\n", msg)
	} else {
		fmt.Fprintf(a.Err, "✓ %s\n", msg)
	}
}

// Render prints data in the selected format to stdout or --out-file.
func (a *App) Render(data any, raw []byte, resource string) error {
	out := a.Out
	if a.OutFile != "" {
		f, err := os.Create(expandHome(a.OutFile))
		if err != nil {
			return apperr.New(apperr.General, "cannot write %s: %v", a.OutFile, err)
		}
		defer func() {
			f.Close()
			a.Hint("Wrote " + a.OutFile)
		}()
		out = f
	}
	var cols []string
	if a.Columns != "" {
		for _, c := range strings.Split(a.Columns, ",") {
			if c = strings.TrimSpace(c); c != "" {
				cols = append(cols, c)
			}
		}
	}
	width := 0
	tty := a.OutTTY && a.OutFile == ""
	if tty {
		if f, ok := a.Out.(*os.File); ok {
			width, _, _ = term.GetSize(int(f.Fd()))
		}
	}
	r := &output.Renderer{
		Format: a.format(), Out: out, TTY: tty, Width: width, Columns: cols, Wide: a.Wide,
		Resource: resource, Hint: a.Hint,
		Color: tty && !a.NoColor && os.Getenv("NO_COLOR") == "",
	}
	return r.Render(data, raw, a.Field)
}

// Confirm asks before destructive actions; --yes skips, non-interactive fails.
func (a *App) Confirm(prompt string) error {
	if a.Yes {
		return nil
	}
	if !a.interactive() {
		return apperr.Usagef("%s — refusing without confirmation; re-run with --yes", prompt)
	}
	fmt.Fprintf(a.Err, "%s. Continue? [y/N] ", prompt)
	ans, _ := a.readLine()
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "y", "yes", "j", "ja":
		return nil
	}
	return apperr.New(apperr.Cancelled, "aborted")
}

func (a *App) readLine() (string, error) {
	if a.stdin == nil {
		a.stdin = bufio.NewReader(a.In)
	}
	s, err := a.stdin.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// Prompt asks for a value (hidden input for secrets).
func (a *App) Prompt(label, def string, secret bool) (string, error) {
	if !a.interactive() {
		return "", apperr.Usagef("%s is required (interactive input disabled)", label)
	}
	if def != "" {
		fmt.Fprintf(a.Err, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(a.Err, "%s: ", label)
	}
	var val string
	if secret {
		f, ok := a.In.(*os.File)
		if !ok {
			return "", apperr.Usagef("cannot read %s without a terminal", label)
		}
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(a.Err)
		if err != nil {
			return "", apperr.Usagef("reading %s: %v", label, err)
		}
		val = string(b)
	} else {
		s, err := a.readLine()
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		val = s
	}
	val = strings.TrimSpace(val)
	if val == "" {
		val = def
	}
	return val, nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

// Execute runs the CLI with args and returns the process exit code.
func (a *App) Execute(args []string) int {
	root := NewRootCmd(a)
	root.SetArgs(args)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	root.SetIn(a.In)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		// Errors not produced by our code come from cobra: unknown command, bad args, …
		ae = &apperr.Error{Msg: err.Error(), Code: apperr.Usage}
	}
	if a.machineOutput() && !a.ErrTTY {
		e := ojson.NewObject()
		e.Set("error", ae.Msg)
		e.Set("exit_code", ae.Code)
		if ae.Status != 0 {
			e.Set("status", ae.Status)
		}
		if ae.Details != nil {
			e.Set("details", ae.Details)
		}
		b, _ := ojson.Marshal(e)
		fmt.Fprintln(a.Err, string(b))
	} else {
		fmt.Fprintln(a.Err, "error: "+ae.Msg)
		if ae.Code == apperr.Usage && strings.Contains(ae.Msg, "unknown command") {
			fmt.Fprintln(a.Err, "Run 'epm-cli --help' for usage.")
		}
	}
	return ae.Code
}

// validateGlobals runs before every command.
func (a *App) validateGlobals(cmd *cobra.Command) error {
	if a.Output != "" && !slices.Contains(output.Formats, a.Output) {
		return apperr.Usagef("invalid --output %q (choose from %s)", a.Output, strings.Join(output.Formats, ", "))
	}
	return nil
}
