/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"io"
	slog "log"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/fang"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/spf13/cobra"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	dctx "github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
	BuiltBy = ""
)

var (
	cfgFlag string

	logo = lipgloss.NewStyle().Foreground(dctx.LogoColor).MarginBottom(1).SetString(constants.Logo)

	rootCmd = &cobra.Command{
		Use: "gh dash",
		Long: lipgloss.JoinVertical(
			lipgloss.Left,
			logo.Render(),
			"A rich terminal UI for GitHub that doesn't break your flow.",
			"",
			lipgloss.NewStyle().
				Faint(true).
				Italic(true).
				Render("Visit https://gh-dash.dev for the docs."),
		),
		Short:   "A rich terminal UI for GitHub that doesn't break your flow.",
		Version: "",
		Example: `
# Running without arguments will either:
#   - Use the global configuration file
#   - Use a local .gh-dash.yml file if in a git repo
gh dash

# Run with a specific configuration file
gh dash --config /path/to/configuration/file.yml

# Run with debug logging to debug.log
gh dash --debug

# Print version
gh dash -v
	`,
		Args: cobra.MaximumNArgs(1),
	}
)

func Execute() {
	themeFunc := fang.WithColorSchemeFunc(func(
		ld lipgloss.LightDarkFunc,
	) fang.ColorScheme {
		c := ld(lipgloss.Color("#00196F"), lipgloss.Color("#02F9FB"))
		def := fang.DefaultColorScheme(ld)
		def.DimmedArgument = ld(lipgloss.Black, lipgloss.White)
		def.Codeblock = lipgloss.Color("#1E1E2C")
		def.Title = c
		def.Flag = lipgloss.Color("#42A0FA")
		def.Command = c
		def.Program = c
		return def
	})
	if err := fang.Execute(
		context.Background(),
		rootCmd,
		themeFunc,
		fang.WithVersion(rootCmd.Version),
		fang.WithoutCompletions(),
		fang.WithoutManpage(),
	); err != nil {
		os.Exit(1)
	}
}

func setDebugLogLevel() {
	switch os.Getenv("LOG_LEVEL") {
	case "debug", "":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	}
}

func createModel(location config.Location, debug bool) (tui.Model, *os.File) {
	var loggerFile *os.File

	if debug {
		var fileErr error
		loggerFile, fileErr = os.OpenFile("debug.log",
			os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o666)
		if fileErr == nil {
			log.SetOutput(loggerFile)
			log.SetTimeFormat(time.Kitchen)
			log.SetReportCaller(true)
			setDebugLogLevel()
			log.Info("Logging to debug.log")
			if location.RepoPath != "" {
				log.Info("Running in repo", "repo", location.RepoPath)
			}
		} else {
			loggerFile, _ = tea.LogToFile("debug.log", "debug")
			slog.Print("Failed setting up logging", fileErr)
		}
	} else {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.FatalLevel)
	}

	return tui.NewModel(location), loggerFile
}

func buildVersion(version, commit, date, builtBy string) string {
	result := version
	if commit != "" {
		result = fmt.Sprintf("%s\ncommit: %s", result, commit)
	}
	if date != "" {
		result = fmt.Sprintf("%s\nbuilt at: %s", result, date)
	}
	if builtBy != "" {
		result = fmt.Sprintf("%s\nbuilt by: %s", result, builtBy)
	}
	result = fmt.Sprintf("%s\ngoos: %s\ngoarch: %s", result, runtime.GOOS, runtime.GOARCH)
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Sum != "" {
		result = fmt.Sprintf(
			"%s\nmodule version: %s, checksum: %s",
			result,
			info.Main.Version,
			info.Main.Sum,
		)
	}

	return result
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&cfgFlag,
		"config",
		"c",
		"",
		`use this configuration file
(default lookup:
  1. a .gh-dash.yml file if inside a git repo
  2. $GH_DASH_CONFIG env var
  3. $XDG_CONFIG_HOME/gh-dash/config.yml
)`,
	)
	err := rootCmd.MarkPersistentFlagFilename("config", "yaml", "yml")
	if err != nil {
		log.Fatal("Cannot mark config flag as filename", err)
	}

	rootCmd.Version = buildVersion(Version, Commit, Date, BuiltBy)
	rootCmd.SetVersionTemplate(
		lipgloss.JoinVertical(
			lipgloss.Left,
			"",
			logo.Render(),
			`gh-dash {{printf "version %s\n" .Version}}`,
		),
	)

	rootCmd.Flags().Bool(
		"debug",
		false,
		"passing this flag will allow writing debug output to debug.log",
	)

	rootCmd.Flags().Int(
		"dump-render",
		0,
		"DEBUG: read a markdown body from stdin, render it through the full prview pipeline at the given width (and the corresponding sidebar/Padding/viewport wrapping), then write the raw bytes (with ANSI escapes literal) to stdout and exit. Used to inspect what bytes the binary actually emits for code-block backgrounds.",
	)

	rootCmd.Flags().String(
		"cpuprofile",
		"",
		"write cpu profile to file",
	)

	rootCmd.Flags().BoolP(
		"help",
		"h",
		false,
		"help for gh-dash",
	)

	rootCmd.Run = func(_ *cobra.Command, args []string) {
		// Debug pipeline dump short-circuits the TUI startup. Reads
		// markdown body from stdin, renders through the same path as
		// prview.renderSummary (markdown.Render → outer Width wrap →
		// outer Padding wrapper → simulated viewport.View Width wrap),
		// then prints each output line with ANSI escapes shown
		// literally so we can inspect the bytes the binary actually
		// emits to the terminal.
		dumpWidth, _ := rootCmd.Flags().GetInt("dump-render")
		if dumpWidth > 0 {
			runDumpRender(dumpWidth)
			return
		}

		var repo string
		repos := config.IsFeatureEnabled(config.FF_REPO_VIEW)
		if repos && len(args) > 0 {
			repo = args[0]
		}

		if repo == "" {
			r, err := git.GetRepoInPwd()
			if err == nil && r != nil {
				repo = r.Path()
			}
		}
		debug, err := rootCmd.Flags().GetBool("debug")
		if err != nil {
			log.Fatal("Cannot parse debug flag", err)
		}

		zone.NewGlobal()

		model, logger := createModel(config.Location{RepoPath: repo, ConfigFlag: cfgFlag}, debug)
		if logger != nil {
			defer logger.Close()
		}

		cpuprofile, err := rootCmd.Flags().GetString("cpuprofile")
		if err != nil {
			log.Fatal("Cannot parse cpuprofile flag", err)
		}
		if cpuprofile != "" {
			f, err := os.Create(cpuprofile)
			if err != nil {
				log.Fatal(err)
			}
			_ = pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}

		p := tea.NewProgram(model)
		if _, err := p.Run(); err != nil {
			log.Fatal("Failed starting the TUI", err)
		}
	}
}

// runDumpRender mirrors the rendering pipeline that prview.renderSummary
// drives at runtime: markdown.Render(width, body) → inner Width wrap →
// outer Padding wrapper (the +ContentPadding around body) → simulated
// viewport.View Width wrap. It reads the markdown body from stdin and
// writes one line per output line to stdout, with ANSI escapes shown
// literally (\x1b[...) so we can see the SGR sequences and the
// background-padded trailing spaces (or lack thereof). Exits when done.
func runDumpRender(width int) {
	markdown.InitializeMarkdownStyle(true)
	bodyBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read stdin:", err)
		os.Exit(1)
	}
	body := string(bodyBytes)
	if strings.TrimSpace(body) == "" {
		body = "Sample\n\n```xml\n<filter>\n  <acl>foo</acl>\n</filter>\n```\n\nDone."
		fmt.Fprintln(os.Stderr, "(no stdin; using built-in xml sample)")
	}

	rendered, err := markdown.Render(width, body)
	if err != nil {
		fmt.Fprintln(os.Stderr, "markdown.Render:", err)
		os.Exit(1)
	}

	// Mirror prview.renderSummary's Width wrap, then prview.View's
	// Padding wrapper, then the viewport's outer Width(viewportWidth).
	innerWrap := lipgloss.NewStyle().Width(width).MaxWidth(width).Align(lipgloss.Left).Render(rendered)
	contentPadding := 2 // gh-dash default Sidebar.ContentPadding
	withPadding := lipgloss.NewStyle().Padding(0, contentPadding).Render(innerWrap)
	viewportWidth := width + 2*contentPadding
	final := lipgloss.NewStyle().Width(viewportWidth).Render(withPadding)

	fmt.Fprintf(os.Stdout, "=== width=%d viewportWidth=%d ===\n", width, viewportWidth)
	for i, line := range strings.Split(final, "\n") {
		visW := lipgloss.Width(line)
		bg := strings.Contains(line, "\x1b[48;2;234;238;242")
		// Show escapes literally so SGR sequences are visible.
		literal := strings.ReplaceAll(line, "\x1b", "\\x1b")
		fmt.Fprintf(os.Stdout, "%2d w=%d bg=%v : %s\n", i, visW, bg, literal)
	}
}
