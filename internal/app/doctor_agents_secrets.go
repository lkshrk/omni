package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const harnessOpenCode = "opencode"

var (
	dollarBraceReference = regexp.MustCompile(`\$\{[^{}]+\}`)
	openCodeEnvReference = regexp.MustCompile(`\{env:[^{}]+\}`)
)

type clientMCPSecrets struct {
	Env     map[string]string
	Headers map[string]string
}

func (a *App) doctorAgentsSecretLiterals(result *DoctorResult, home string, lock apmLockfile) {
	const label = "MCP secrets"
	clients := []struct {
		name      string
		path      string
		reference *regexp.Regexp
		read      func(string) (map[string]clientMCPSecrets, error)
	}{
		{harnessClaude, claudeConfigFileIn(home), dollarBraceReference, readJSONMCPSecrets("mcpServers", "env")},
		{harnessOmp, ompMCPConfigFileIn(home), dollarBraceReference, readJSONMCPSecrets("mcpServers", "env")},
		{harnessOpenCode, filepath.Join(home, ".config", "opencode", "opencode.json"), openCodeEnvReference, readJSONMCPSecrets("mcp", "environment")},
		{harnessCodex, filepath.Join(home, ".codex", "config.toml"), nil, readCodexMCPSecrets},
	}
	var findings, unreadable []string
	for _, client := range clients {
		servers := lock.MCPTargetServers[client.name]
		if len(servers) == 0 {
			continue
		}
		entries, err := client.read(client.path)
		if err != nil {
			unreadable = append(unreadable, client.name+" config could not be checked: "+client.path)
			continue
		}
		for _, server := range servers {
			entry, ok := entries[server]
			if !ok {
				continue
			}
			expected := lock.MCPConfigs[server]
			for _, field := range []struct {
				kind     string
				got      map[string]string
				expected map[string]string
			}{
				{"env", entry.Env, expected.Env},
				{"headers", entry.Headers, expected.Headers},
			} {
				for key, value := range field.got {
					if want, ok := field.expected[key]; ok && value == want {
						continue
					}
					if client.reference != nil && client.reference.MatchString(value) {
						continue
					}
					findings = append(findings, fmt.Sprintf("%s (%s) %s %s", server, client.name, field.kind, key))
				}
			}
		}
	}
	sort.Strings(findings)
	switch {
	case len(findings) > 0:
		result.addCheck("agents", label, DoctorStatusWarn, "possibly resolved secret values in client configs", append(findings, unreadable...)...)
	case len(unreadable) > 0:
		result.addCheck("agents", label, DoctorStatusWarn, "some client configs could not be checked", unreadable...)
	default:
		result.addCheck("agents", label, DoctorStatusOK, "MCP env and header values are references")
	}
}

func readJSONMCPSecrets(serversKey, envKey string) func(string) (map[string]clientMCPSecrets, error) {
	return func(path string) (map[string]clientMCPSecrets, error) {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil || info.Size() > harnessClaudeMaxKiB {
			return nil, fmt.Errorf("unreadable")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("unreadable")
		}
		var doc map[string]json.RawMessage
		if json.Unmarshal(data, &doc) != nil {
			return nil, fmt.Errorf("unparseable")
		}
		var servers map[string]map[string]json.RawMessage
		if raw, ok := doc[serversKey]; ok && json.Unmarshal(raw, &servers) != nil {
			return nil, fmt.Errorf("unparseable")
		}
		out := make(map[string]clientMCPSecrets, len(servers))
		for name, fields := range servers {
			out[name] = clientMCPSecrets{Env: jsonStringMap(fields[envKey]), Headers: jsonStringMap(fields["headers"])}
		}
		return out, nil
	}
}

func jsonStringMap(raw json.RawMessage) map[string]string {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		var s string
		if json.Unmarshal(value, &s) == nil {
			out[key] = s
		}
	}
	return out
}

// Every value under [mcp_servers.<server>.env] or .http_headers is a literal: Codex takes references via env_vars and env_http_headers.
func readCodexMCPSecrets(path string) (map[string]clientMCPSecrets, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unreadable")
	}
	defer func() { _ = file.Close() }()
	out := map[string]clientMCPSecrets{}
	var current map[string]string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			current = nil
			server, kind, ok := codexMCPSubtable(line)
			if !ok {
				continue
			}
			entry := out[server]
			switch kind {
			case "env":
				if entry.Env == nil {
					entry.Env = map[string]string{}
				}
				current = entry.Env
			case "http_headers":
				if entry.Headers == nil {
					entry.Headers = map[string]string{}
				}
				current = entry.Headers
			}
			out[server] = entry
			continue
		}
		if current == nil {
			continue
		}
		if key, value, ok := codexStringAssignment(line); ok {
			current[key] = value
		}
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("unreadable")
	}
	return out, nil
}

func codexMCPSubtable(line string) (server, kind string, ok bool) {
	if idx := strings.Index(line, "#"); idx >= 0 && !strings.Contains(line[:idx], "\"") {
		line = strings.TrimSpace(line[:idx])
	}
	if !strings.HasPrefix(line, "[") || strings.HasPrefix(line, "[[") || !strings.HasSuffix(line, "]") {
		return "", "", false
	}
	body := strings.TrimSpace(line[1 : len(line)-1])
	for _, kind := range []string{"env", "http_headers"} {
		name, found := strings.CutSuffix(body, "."+kind)
		if !found {
			continue
		}
		if server, ok := codexMCPTableName("[" + name + "]"); ok {
			return server, kind, true
		}
	}
	return "", "", false
}

func codexStringAssignment(line string) (string, string, bool) {
	key, rest, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if unquoted, err := strconv.Unquote(key); err == nil {
		key = unquoted
	}
	rest = strings.TrimSpace(rest)
	if key == "" || rest == "" {
		return "", "", false
	}
	switch rest[0] {
	case '"':
		end := 1
		for end < len(rest) && rest[end] != '"' {
			if rest[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(rest) {
			return "", "", false
		}
		value, err := strconv.Unquote(rest[:end+1])
		if err != nil {
			return key, rest[1:end], true
		}
		return key, value, true
	case '\'':
		end := strings.IndexByte(rest[1:], '\'')
		if end < 0 {
			return "", "", false
		}
		return key, rest[1 : end+1], true
	}
	return "", "", false
}
