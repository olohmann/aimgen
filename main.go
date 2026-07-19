package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Exit codes.
const (
	exitOK       = 0
	exitUsage    = 1
	exitAPIError = 2
)

// usageText is printed for --help and on usage errors.
const usageText = `aimgen - generate images via the Azure AI Foundry images endpoint

Usage:
  aimgen [flags] <prompt>
  aimgen [flags] --prompt "<prompt>"
  aimgen [flags] --image input.png <prompt>

Flags:
  --prompt string       Prompt text (or pass as a positional argument)
  --image string        Input image to edit; repeat --image for multiple inputs.
                        When set, aimgen calls the edits endpoint instead of generation.
  --mask string         PNG mask for inpainting (requires --image)
  --token string        Bearer token (env: AZURE_AI_TOKEN)
  --token-command string  Shell command that prints the bearer token (env: AZURE_AI_TOKEN_COMMAND)
  --endpoint string     API base endpoint (env: AIMGEN_ENDPOINT)
  --model string        Model deployment name (env: AIMGEN_MODEL)
  --size string         Image size, e.g. 1024x1024
  --format string       Output format: png, jpeg, webp
  -n int                Number of images to generate
  --compression int     Output compression (0-100)
  -o, --out string      Output path stem (index suffix added when n>1)
  --timeout int         HTTP timeout in seconds
  --config string       Path to config file
  --init-config         Write a commented sample config and exit
  --quiet               Disable the spinner
  --verbose             Log request/response summary (token redacted)
  --version             Print version information and exit
  --help                Show this help

Iterative refinement:
  Refine an image over multiple turns by chaining runs, feeding each output back
  in as the next --image:
    aimgen --image a.png -o b.png "make the sky purple"
    aimgen --image b.png -o c.png "add a rainbow"

Configuration precedence (highest -> lowest):
  CLI flag -> environment variable -> config file -> built-in default
Default config: $XDG_CONFIG_HOME/aimgen/config.toml (~/.config/aimgen/config.toml)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns an exit code. It is separated from main for
// testability.
func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("aimgen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }

	var (
		promptFlag   = fs.String("prompt", "", "")
		token        = fs.String("token", "", "")
		tokenCommand = fs.String("token-command", "", "")
		endpoint     = fs.String("endpoint", "", "")
		model        = fs.String("model", "", "")
		size         = fs.String("size", "", "")
		format       = fs.String("format", "", "")
		mask         = fs.String("mask", "", "")
		count        = fs.Int("n", 0, "")
		compression  = fs.Int("compression", 0, "")
		outLong      = fs.String("out", "", "")
		outShort     = fs.String("o", "", "")
		timeout      = fs.Int("timeout", 0, "")
		configPath   = fs.String("config", "", "")
		initConfig   = fs.Bool("init-config", false, "")
		quiet        = fs.Bool("quiet", false, "")
		verbose      = fs.Bool("verbose", false, "")
		showVersion  = fs.Bool("version", false, "")
		help         = fs.Bool("help", false, "")
	)

	var images stringSlice
	fs.Var(&images, "image", "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stderr, usageText)
			return exitOK
		}
		return exitUsage
	}

	if *help {
		fmt.Fprint(stderr, usageText)
		return exitOK
	}

	if *showVersion {
		fmt.Fprintln(stdout, versionString())
		return exitOK
	}

	// Resolve config file path.
	path := *configPath
	if path == "" {
		p, err := defaultConfigPath()
		if err != nil {
			fmt.Fprintf(stderr, "aimgen: cannot resolve config path: %v\n", err)
			return exitUsage
		}
		path = p
	}

	if *initConfig {
		if err := writeSampleConfig(path); err != nil {
			fmt.Fprintf(stderr, "aimgen: %v\n", err)
			return exitUsage
		}
		fmt.Fprintf(stderr, "Wrote sample config to %s\n", path)
		return exitOK
	}

	flags := stringFlags{
		endpoint:     *endpoint,
		token:        *token,
		tokenCommand: *tokenCommand,
		model:        *model,
		size:         *size,
		format:       *format,
		count:        *count,
		countSet:     flagSet(fs, "n"),
		compression:  *compression,
		compSet:      flagSet(fs, "compression"),
		timeout:      *timeout,
		timeoutSet:   flagSet(fs, "timeout"),
	}

	cfg, err := resolveConfig(path, flags, os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "aimgen: %v\n", err)
		return exitUsage
	}

	resolvedToken, err := resolveToken(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "aimgen: %v\n", err)
		return exitUsage
	}
	cfg.Token = resolvedToken

	prompt := resolvePrompt(*promptFlag, fs.Args())
	if prompt == "" {
		fmt.Fprintln(stderr, "aimgen: a prompt is required (positional argument or --prompt)")
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}

	if cfg.Token == "" {
		fmt.Fprintln(stderr, "aimgen: no token provided (use --token, $AZURE_AI_TOKEN, --token-command, or config)")
		return exitUsage
	}

	if cfg.Count < 1 {
		fmt.Fprintln(stderr, "aimgen: -n must be at least 1")
		return exitUsage
	}

	if *mask != "" && len(images) == 0 {
		fmt.Fprintln(stderr, "aimgen: --mask requires at least one --image")
		return exitUsage
	}
	for _, p := range images {
		if err := checkReadable(p); err != nil {
			fmt.Fprintf(stderr, "aimgen: --image %v\n", err)
			return exitUsage
		}
	}
	if *mask != "" {
		if err := checkReadable(*mask); err != nil {
			fmt.Fprintf(stderr, "aimgen: --mask %v\n", err)
			return exitUsage
		}
	}
	editing := len(images) > 0

	outStem := firstNonEmpty(*outShort, *outLong, defaultOutput)

	if *verbose {
		logVerbose(stderr, cfg, prompt, outStem, images, *mask)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	httpClient := &http.Client{}
	c := newClient(httpClient, cfg)

	label := "Generating image"
	if editing {
		label = "Editing image"
	}
	sp := newSpinner(stderr, label, *quiet)
	sp.Start()
	var results [][]byte
	var genErr error
	if editing {
		results, genErr = c.edit(ctx, prompt, images, *mask)
	} else {
		results, genErr = c.generate(ctx, prompt)
	}
	sp.Stop()

	if genErr != nil {
		var ae *apiErr
		if errors.As(genErr, &ae) {
			fmt.Fprintf(stderr, "aimgen: API error: %v\n", ae)
			if *verbose && ae.raw != "" {
				fmt.Fprintf(stderr, "aimgen: raw response: %s\n", ae.raw)
			}
			return exitAPIError
		}
		fmt.Fprintf(stderr, "aimgen: %v\n", genErr)
		return exitAPIError
	}

	paths, err := writeImages(outStem, results)
	if err != nil {
		fmt.Fprintf(stderr, "aimgen: %v\n", err)
		return exitAPIError
	}

	for _, p := range paths {
		fmt.Fprintln(stdout, p)
	}
	return exitOK
}

// stringSlice is a flag.Value that accumulates repeated string flags, allowing
// --image to be passed multiple times.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// checkReadable verifies a path exists and is a regular, openable file.
func checkReadable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s: is a directory", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// resolvePrompt picks the prompt from the --prompt flag or positional args.
func resolvePrompt(promptFlag string, args []string) string {
	if promptFlag != "" {
		return promptFlag
	}
	return strings.TrimSpace(strings.Join(args, " "))
}

// flagSet reports whether the named flag was explicitly set on the command line.
func flagSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// logVerbose prints a redacted request summary to stderr.
func logVerbose(stderr *os.File, cfg Config, prompt, out string, images []string, mask string) {
	apiPath := cfg.APIPath
	if len(images) > 0 {
		apiPath = cfg.EditPath
	}
	fmt.Fprintf(stderr, "aimgen: endpoint=%s%s model=%s size=%s format=%s n=%d compression=%d timeout=%ds token=%s out=%s\n",
		strings.TrimRight(cfg.Endpoint, "/"), apiPath, cfg.Model, cfg.Size, cfg.Format,
		cfg.Count, cfg.Compression, cfg.Timeout, redact(cfg.Token), out)
	if len(images) > 0 {
		fmt.Fprintf(stderr, "aimgen: mode=edit images=%v mask=%q\n", images, mask)
	}
	fmt.Fprintf(stderr, "aimgen: prompt=%q\n", prompt)
}

// redact masks a token, keeping only a short prefix for identification.
func redact(token string) string {
	if token == "" {
		return "(none)"
	}
	if len(token) <= 6 {
		return "***"
	}
	return token[:4] + "…(redacted)"
}
