package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// KV is an ordered key/value pair (env or header).
type KV struct {
	Key   string
	Value string
}

// Server is one normalized [[servers]] row: a server entry expanded to exactly
// one scope. Entries that declare multiple scopes produce multiple rows, in
// declaration order.
type Server struct {
	Name              string
	Type              string
	Command           string
	URL               string
	Args              []string
	Agents            []string
	Profiles          []string
	Env               []KV
	Headers           []KV
	EnvVars           []string
	BearerTokenEnvVar string
	StartupTimeoutSec *int
	Scope             string
}

// SecretResolver resolves a ${VAR} placeholder: secrets.toml (exact, then
// lowercase) first, then the environment. Missing keys yield "".
type SecretResolver func(key string) string

// ParseServers reads and validates the unified config's [[servers]] table,
// expanding ${VAR} placeholders in url/args/env/headers at parse time.
func ParseServers(path string, resolve SecretResolver) ([]Server, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s: manifest not found: %s", Prog, path)
		}
		return nil, fmt.Errorf("%s: cannot read manifest %s: %v", Prog, path, err)
	}

	var raw map[string]any
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("%s: manifest is not valid TOML: %s: %v", Prog, path, err)
	}

	serversVal, ok := raw["servers"]
	if !ok {
		return nil, fmt.Errorf("%s: manifest must contain a \"servers\" array: %s", Prog, path)
	}
	entries, ok := toAnyList(serversVal)
	if !ok {
		return nil, fmt.Errorf("%s: manifest must contain a \"servers\" array: %s", Prog, path)
	}

	orders := serverTableOrders(md)
	out := []Server{}
	for i, e := range entries {
		entry, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: each server entry must be a table: %s", Prog, path)
		}
		var order tableOrder
		if i < len(orders) {
			order = orders[i]
		}
		rows, err := parseServerEntry(entry, path, order, resolve)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func toAnyList(v any) ([]any, bool) {
	switch t := v.(type) {
	case []map[string]any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = e
		}
		return out, true
	case []any:
		return t, true
	default:
		return nil, false
	}
}

type tableOrder struct {
	env     []string
	headers []string
}

// serverTableOrders reconstructs the document order of each [[servers]] entry's
// env/headers sub-table keys. Go maps lose order, so we recover it from the
// TOML metadata key stream, which preserves declaration order.
func serverTableOrders(md toml.MetaData) []tableOrder {
	var orders []tableOrder
	cur := -1
	for _, k := range md.Keys() {
		s := k.String()
		if s == "servers" {
			orders = append(orders, tableOrder{})
			cur = len(orders) - 1
			continue
		}
		if cur < 0 {
			continue
		}
		switch {
		case strings.HasPrefix(s, "servers.env."):
			orders[cur].env = append(orders[cur].env, strings.TrimPrefix(s, "servers.env."))
		case strings.HasPrefix(s, "servers.headers."):
			orders[cur].headers = append(orders[cur].headers, strings.TrimPrefix(s, "servers.headers."))
		}
	}
	return orders
}

func parseServerEntry(entry map[string]any, manifestPath string, order tableOrder, resolve SecretResolver) ([]Server, error) {
	rawName, _ := entry["name"].(string)
	name := strings.TrimSpace(rawName)
	if rawName == "" || name == "" {
		return nil, fmt.Errorf("%s: each server entry must include a non-empty \"name\": %s", Prog, manifestPath)
	}

	agents, err := stringListField(entry, "agents", name)
	if err != nil {
		return nil, err
	}
	profiles, err := stringListField(entry, "profiles", name)
	if err != nil {
		return nil, err
	}
	command, err := optionalStringField(entry, "command", name)
	if err != nil {
		return nil, err
	}
	serverType, err := optionalStringField(entry, "type", name)
	if err != nil {
		return nil, err
	}
	url, err := optionalStringField(entry, "url", name)
	if err != nil {
		return nil, err
	}
	args, err := stringListField(entry, "args", name)
	if err != nil {
		return nil, err
	}
	envVars, err := stringListField(entry, "env_vars", name)
	if err != nil {
		return nil, err
	}
	envMap, err := stringMapField(entry, "env", name)
	if err != nil {
		return nil, err
	}
	headersMap, err := stringMapField(entry, "headers", name)
	if err != nil {
		return nil, err
	}
	bearer, err := optionalStringField(entry, "bearer_token_env_var", name)
	if err != nil {
		return nil, err
	}

	var timeout *int
	if tv, ok := entry["startup_timeout_sec"]; ok && tv != nil {
		n, isInt := tv.(int64)
		if !isInt {
			return nil, fmt.Errorf("%s: \"startup_timeout_sec\" must be an int in server: %s", Prog, name)
		}
		v := int(n)
		timeout = &v
	}

	scopes, err := expandServerScopes(entry["scope"], name)
	if err != nil {
		return nil, err
	}

	environ := orderedKV(envMap, order.env)
	headers := orderedKV(headersMap, order.headers)

	base := Server{
		Name:              name,
		Type:              strings.TrimSpace(serverType),
		Command:           strings.TrimSpace(command),
		URL:               expandPlaceholders(strings.TrimSpace(url), resolve),
		Args:              expandAll(args, resolve),
		Agents:            trimNonEmpty(agents),
		Profiles:          trimNonEmpty(profiles),
		Env:               expandKV(environ, resolve),
		Headers:           expandKV(headers, resolve),
		EnvVars:           envVars,
		BearerTokenEnvVar: strings.TrimSpace(bearer),
		StartupTimeoutSec: timeout,
	}

	rows := make([]Server, 0, len(scopes))
	for _, sc := range scopes {
		row := base
		row.Scope = sc
		rows = append(rows, row)
	}
	return rows, nil
}

func stringListField(entry map[string]any, field, name string) ([]string, error) {
	v, ok := entry[field]
	if !ok || v == nil {
		return []string{}, nil
	}
	if s, isStr := v.(string); isStr {
		_ = s
		return nil, fmt.Errorf("%s: \"%s\" must be a string array in server: %s", Prog, field, name)
	}
	arr, isArr := v.([]any)
	if !isArr {
		return nil, fmt.Errorf("%s: \"%s\" must be a string array in server: %s", Prog, field, name)
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, isStr := item.(string)
		if !isStr {
			return nil, fmt.Errorf("%s: \"%s\" must be a string array in server: %s", Prog, field, name)
		}
		out = append(out, s)
	}
	return out, nil
}

func optionalStringField(entry map[string]any, field, name string) (string, error) {
	v, ok := entry[field]
	if !ok || v == nil {
		return "", nil
	}
	s, isStr := v.(string)
	if !isStr {
		return "", fmt.Errorf("%s: \"%s\" must be a string in server: %s", Prog, field, name)
	}
	return s, nil
}

func stringMapField(entry map[string]any, field, name string) (map[string]any, error) {
	v, ok := entry[field]
	if !ok || v == nil {
		return map[string]any{}, nil
	}
	m, isMap := v.(map[string]any)
	if !isMap {
		return nil, fmt.Errorf("%s: \"%s\" must be a string map in server: %s", Prog, field, name)
	}
	for _, val := range m {
		if _, isStr := val.(string); !isStr {
			return nil, fmt.Errorf("%s: \"%s\" must be a string map in server: %s", Prog, field, name)
		}
	}
	return m, nil
}

func expandServerScopes(raw any, name string) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s: each server entry must declare \"scope\" as \"user\" or \"project\": %s", Prog, name)
	}
	var scopes []string
	switch t := raw.(type) {
	case string:
		scopes = []string{strings.TrimSpace(t)}
	case []any:
		if len(t) == 0 {
			return nil, fmt.Errorf("%s: \"scope\" array must not be empty in server: %s", Prog, name)
		}
		for _, item := range t {
			s, isStr := item.(string)
			if !isStr {
				return nil, fmt.Errorf("%s: \"scope\" must be a string or an array of strings in server: %s", Prog, name)
			}
			scopes = append(scopes, strings.TrimSpace(s))
		}
	default:
		return nil, fmt.Errorf("%s: \"scope\" must be a string or an array of strings in server: %s", Prog, name)
	}

	for _, s := range scopes {
		if s == "user" || s == "project" {
			continue
		}
		hint := ""
		if s == "global" {
			hint = " (use \"user\")"
		} else if s == "local" {
			hint = " (use \"project\")"
		}
		return nil, fmt.Errorf("%s: scope must be \"user\" or \"project\", got \"%s\"%s in server: %s", Prog, s, hint, name)
	}

	seen := map[string]bool{}
	out := []string{}
	for _, s := range scopes {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// orderedKV renders a TOML table in document order, appending any keys the
// metadata pass missed in sorted order for determinism.
func orderedKV(m map[string]any, order []string) []KV {
	out := make([]KV, 0, len(m))
	seen := map[string]bool{}
	for _, k := range order {
		v, ok := m[k]
		if !ok {
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			continue
		}
		seen[k] = true
		out = append(out, KV{Key: k, Value: s})
	}
	rest := []string{}
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if s, ok := m[k].(string); ok {
			out = append(out, KV{Key: k, Value: s})
		}
	}
	return out
}

func expandAll(items []string, resolve SecretResolver) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = expandPlaceholders(item, resolve)
	}
	return out
}

func expandKV(items []KV, resolve SecretResolver) []KV {
	out := make([]KV, len(items))
	for i, kv := range items {
		out[i] = KV{Key: kv.Key, Value: expandPlaceholders(kv.Value, resolve)}
	}
	return out
}

// expandPlaceholders mirrors the zsh/python loop: substitute ${VAR} until no
// unclosed placeholder remains. Missing vars resolve to "" silently.
func expandPlaceholders(value string, resolve SecretResolver) string {
	s := value
	for strings.Contains(s, "${") {
		pre, rest, _ := strings.Cut(s, "${")
		if !strings.Contains(rest, "}") {
			break
		}
		varName, post, _ := strings.Cut(rest, "}")
		s = pre + resolve(varName) + post
	}
	return s
}

func trimNonEmpty(items []string) []string {
	out := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// KVsToMap converts an ordered KV slice to a map (last value wins).
func KVsToMap(kvs []KV) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value
	}
	return m
}
