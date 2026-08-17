package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// packaging/mcpb/manifest.json is the MCP bundle's manifest — the file a
// desktop app reads to decide what to run and what to ask the user for.
// Nothing else in this repository reads it, which is exactly the problem:
// a bundle that names a command the binary does not have installs fine
// and fails at the first message, on somebody else's machine, in an app
// whose log nobody thinks to open.
//
// So it is read back here against the build (design doc 0035). The one
// thing not checked is whether a desktop app accepts the manifest, which
// no test in this repository can answer; scripts/mcpb says why the
// official packer is not in the release job.
func mcpbManifest(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../packaging/mcpb/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("the bundle manifest is not JSON: %v", err)
	}
	return m
}

func TestMCPBManifestRunsACommandTheBinaryHas(t *testing.T) {
	m := mcpbManifest(t)

	// The fields MCPB requires. A bundle missing one is refused at
	// install time with a message about the manifest rather than about
	// ochakai.
	for _, key := range []string{"manifest_version", "name", "version", "description", "author", "server"} {
		if _, ok := m[key]; !ok {
			t.Errorf("the manifest has no %q, which MCPB requires", key)
		}
	}

	server, _ := m["server"].(map[string]any)
	if got, _ := server["type"].(string); got != "binary" {
		t.Errorf("server.type = %q, want binary: the bundle carries the compiled CLI", got)
	}
	cfg, _ := server["mcp_config"].(map[string]any)
	args, _ := cfg["args"].([]any)
	if len(args) == 0 {
		t.Fatal("mcp_config.args is empty; the bundle would run the CLI with no command")
	}
	// The subcommand is the whole point of the bundle, and it is spelled
	// in a file no compiler reads.
	sub, _ := args[0].(string)
	if _, ok := clientCommands[sub]; !ok {
		t.Errorf("the bundle runs `ochakai %s`, which is not a command this binary has", sub)
	}
	if sub != "mcp-stdio" {
		t.Errorf("the bundle runs %q; a desktop app speaks stdio and mcp-stdio is the bridge", sub)
	}

	// Every substitution has something to substitute. A typo here leaves
	// the literal "${user_config.urls}" as the server URL.
	userConfig, _ := m["user_config"].(map[string]any)
	for _, a := range args {
		s, _ := a.(string)
		const open = "${user_config."
		i := strings.Index(s, open)
		if i < 0 {
			continue
		}
		key := s[i+len(open):]
		key, _, _ = strings.Cut(key, "}")
		if _, ok := userConfig[key]; !ok {
			t.Errorf("args reference ${user_config.%s}, which user_config does not define", key)
		}
	}
	for key, v := range userConfig {
		opt, _ := v.(map[string]any)
		if req, _ := opt["required"].(bool); req {
			if _, ok := opt["default"]; !ok {
				t.Errorf("user_config.%s is required with no default; the bundle should install "+
					"and run against the public demo without the reader having a server yet", key)
			}
		}
	}

	// Windows gets its own command because the file has a different name,
	// and only that: the manifest cannot vary by CPU, which is why
	// scripts/mcpb settles the architecture before packing.
	overrides, _ := cfg["platform_overrides"].(map[string]any)
	win, _ := overrides["win32"].(map[string]any)
	if got, _ := win["command"].(string); !strings.HasSuffix(got, ".exe") {
		t.Errorf("win32 command = %q, want the .exe scripts/mcpb writes", got)
	}
	// The paths the packing script fills. They are written twice — here
	// and in the script — and this is the end that fails first.
	if got, _ := server["entry_point"].(string); got != "server/ochakai" {
		t.Errorf("entry_point = %q, want server/ochakai (scripts/mcpb writes it there)", got)
	}
	// A command is spawned from whatever directory the desktop app happens
	// to be in, not from the unpacked bundle, so a bare relative path
	// installs fine and then dies with "No such file or directory" the
	// first time the app starts the server. ${__dirname} is the bundle's
	// own directory, and it is the only thing that makes the path absolute
	// at spawn time.
	commands := map[string]any{"command": cfg["command"], "platform_overrides.win32.command": win["command"]}
	for where, v := range commands {
		command, _ := v.(string)
		if !strings.HasPrefix(command, "${__dirname}/server/ochakai") {
			t.Errorf("%s = %q, want it under ${__dirname}/server/: a relative command is "+
				"resolved against the desktop app's own directory and fails to spawn", where, command)
		}
	}

	// scripts/mcpb stamps the release version over this exact string.
	if got, _ := m["version"].(string); got != "0.0.0" {
		t.Errorf("version = %q, want the 0.0.0 placeholder scripts/mcpb replaces", got)
	}
}
