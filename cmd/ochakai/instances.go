// `ochakai use` and `ochakai whoami`: pick the server client commands
// talk to without carrying $OCHAKAI_URL around (design doc 0004 §2
// decision 5). Explicit settings always win — --url, then $OCHAKAI_URL,
// then the saved selection — so scripts and CI never inherit hidden
// state from a human's config file.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// cliConfig is ~/.config/ochakai/config.json ($XDG_CONFIG_HOME honored):
// named server URLs plus which one is current. URLs only — never tokens.
type cliConfig struct {
	Current   string            `json:"current,omitempty"`
	Instances map[string]string `json:"instances,omitempty"`
}

func (c *cliConfig) currentURL() string { return c.Instances[c.Current] }

func (c *cliConfig) names() []string {
	names := make([]string, 0, len(c.Instances))
	for n := range c.Instances {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func cliConfigPath() (string, error) { return configPathFor(runtime.GOOS) }

// configPathFor resolves the config location the way gh and gcloud do:
// an explicit $XDG_CONFIG_HOME wins everywhere, Windows falls back to
// %AppData%, everyone else to ~/.config.
func configPathFor(goos string) (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" && goos == "windows" {
		dir = os.Getenv("AppData")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "ochakai", "config.json"), nil
}

func loadCLIConfig() (*cliConfig, error) {
	cfg := &cliConfig{Instances: map[string]string{}}
	path, err := cliConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Instances == nil {
		cfg.Instances = map[string]string{}
	}
	return cfg, nil
}

func saveCLIConfig(cfg *cliConfig) error {
	path, err := cliConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// defaultURL is the --url default: $OCHAKAI_URL, else the `use` selection.
// Config errors are swallowed here (a broken file must not take every
// command down); `ochakai use` surfaces them.
func defaultURL() string {
	if v := os.Getenv("OCHAKAI_URL"); v != "" {
		return v
	}
	if cfg, err := loadCLIConfig(); err == nil {
		return cfg.currentURL()
	}
	return ""
}

func cmdUse(_ context.Context, args []string) error {
	fs := newBareFlagSet(
		"Usage: ochakai use [flags] [name | url]\n\nPick the server later client commands talk to, saved to\n~/.config/ochakai/config.json ($XDG_CONFIG_HOME honored;\n%AppData%\\ochakai on Windows).\nWith a URL: save it (named by --name, default its host) and switch.\nWith a name: switch to a saved server. With no argument: list.\n--url and $OCHAKAI_URL always override the saved selection.",
		"  ochakai use http://localhost:8080 --name local\n  ochakai use https://ochakai-prod.run.app --name prod\n  ochakai use prod\n")
	name := fs.String("name", "", "name to save the URL under (default: its host)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cfg, err := loadCLIConfig()
	if err != nil {
		return err
	}
	switch len(pos) {
	case 0:
		if len(cfg.Instances) == 0 {
			fmt.Println("no servers saved; run `ochakai use <url>`")
			return nil
		}
		for _, n := range cfg.names() {
			marker := " "
			if n == cfg.Current {
				marker = "*"
			}
			fmt.Printf("%s %s\t%s\n", marker, n, cfg.Instances[n])
		}
	case 1:
		arg := pos[0]
		if strings.Contains(arg, "://") {
			u, err := url.Parse(arg)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("invalid server URL %q (want http(s)://host)", arg)
			}
			n := *name
			if n == "" {
				n = u.Host
			}
			cfg.Instances[n] = strings.TrimRight(arg, "/")
			cfg.Current = n
		} else {
			if _, ok := cfg.Instances[arg]; !ok {
				return fmt.Errorf("unknown server %q (saved: %s); save one with `ochakai use <url> --name %s`",
					arg, strings.Join(cfg.names(), ", "), arg)
			}
			cfg.Current = arg
		}
		if err := saveCLIConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("using %s (%s)\n", cfg.Current, cfg.currentURL())
	default:
		fs.Usage()
		return errReported
	}
	if env := os.Getenv("OCHAKAI_URL"); env != "" {
		fmt.Fprintf(os.Stderr, "note: $OCHAKAI_URL=%s is set and takes precedence over this selection\n", env)
	}
	return nil
}

func cmdWhoami(ctx context.Context, args []string) error {
	fs, target := newFlagSet(
		"Usage: ochakai whoami [flags]\n\nPrint which server client commands target and where that choice came\nfrom (--url / $OCHAKAI_URL / `ochakai use`), the identity your\ncredentials present (the server's actor resolution is authoritative),\nand whether the server is reachable.",
		"  ochakai whoami\n  ochakai whoami --json\n")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		fs.Usage()
		return errReported
	}
	if *target == "" {
		return errors.New("no server selected: run `ochakai use <url>`, set OCHAKAI_URL, or pass --url")
	}

	report := struct {
		URL      string `json:"url"`
		Source   string `json:"source"`
		Identity string `json:"identity,omitempty"`
		Auth     string `json:"auth,omitempty"`
		Error    string `json:"error,omitempty"`
		Health   string `json:"health"`
	}{URL: *target, Source: urlSource(fs)}

	c, err := newClient(ctx, *target)
	if err != nil {
		report.Error = err.Error()
		report.Health = "skipped (no credentials)"
	} else {
		var actor string
		if actor, report.Auth, err = c.Identity(); err != nil {
			report.Error = err.Error()
		} else {
			report.Identity = actor
		}
		if err := c.Health(ctx); err != nil {
			report.Health = "error: " + err.Error()
		} else {
			report.Health = "ok"
		}
	}

	if *asJSON {
		if err := printJSON(report); err != nil {
			return err
		}
	} else {
		fmt.Printf("server:    %s (%s)\n", report.URL, report.Source)
		if report.Error != "" {
			fmt.Printf("identity:  error: %s\n", report.Error)
		} else {
			fmt.Printf("identity:  %s (%s)\n", report.Identity, report.Auth)
		}
		fmt.Printf("health:    %s\n", report.Health)
	}
	if report.Error != "" || report.Health != "ok" {
		return errReported // details are already on screen
	}
	return nil
}

// urlSource names where the effective --url value came from, mirroring
// the resolution order --url > $OCHAKAI_URL > config.
func urlSource(fs *flag.FlagSet) string {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "url" {
			explicit = true
		}
	})
	switch {
	case explicit:
		return "--url"
	case os.Getenv("OCHAKAI_URL") != "":
		return "$OCHAKAI_URL"
	}
	if cfg, err := loadCLIConfig(); err == nil && cfg.Current != "" && cfg.currentURL() != "" {
		return fmt.Sprintf("ochakai use %q", cfg.Current)
	}
	return "unknown"
}
