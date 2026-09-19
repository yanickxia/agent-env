package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yanickxia/agent-env/internal/config"
)

func normalizeType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "stdio"
	}
	if t == "streamable-http" {
		return "http"
	}
	return t
}

func tomlStr(s string) string { return pyJSONString(s, true) }

func yamlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// parseHeadersToBearerEnv recovers a bearer_token_env_var from an
// Authorization: Bearer ${VAR} header, matching the zsh helper.
func parseHeadersToBearerEnv(headers []config.KV) string {
	auth := ""
	for _, h := range headers {
		if h.Key == "Authorization" || h.Key == "authorization" {
			auth = h.Value
			break
		}
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	token := strings.TrimSpace(auth[len("Bearer "):])
	if strings.HasPrefix(token, "${") && strings.HasSuffix(token, "}") && len(token) > 3 {
		return strings.TrimSpace(token[2 : len(token)-1])
	}
	return ""
}

func sortedKV(kvs []config.KV) []config.KV {
	out := append([]config.KV{}, kvs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func sortServersByName(entries []config.Server) []config.Server {
	out := append([]config.Server{}, entries...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// renderCodex renders the project/user Codex TOML marker block.
func renderCodex(entries []config.Server) string {
	out := []string{codexBegin}
	for _, e := range sortServersByName(entries) {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		t := normalizeType(e.Type)
		out = append(out, "[mcp_servers."+name+"]")

		if t == "stdio" {
			if cmd := strings.TrimSpace(e.Command); cmd != "" {
				out = append(out, "command = "+tomlStr(cmd))
			}
			if len(e.Args) > 0 {
				parts := make([]string, len(e.Args))
				for i, a := range e.Args {
					parts[i] = tomlStr(a)
				}
				out = append(out, "args = ["+strings.Join(parts, ", ")+"]")
			}
			if len(e.EnvVars) > 0 {
				parts := make([]string, len(e.EnvVars))
				for i, v := range e.EnvVars {
					parts[i] = tomlStr(v)
				}
				out = append(out, "env_vars = ["+strings.Join(parts, ", ")+"]")
			}
		} else {
			if url := strings.TrimSpace(e.URL); url != "" {
				out = append(out, "url = "+tomlStr(url))
			}
			bt := strings.TrimSpace(e.BearerTokenEnvVar)
			if bt == "" {
				bt = parseHeadersToBearerEnv(e.Headers)
			}
			if bt != "" {
				out = append(out, "bearer_token_env_var = "+tomlStr(bt))
			}
		}

		if e.StartupTimeoutSec != nil {
			out = append(out, fmt.Sprintf("startup_timeout_sec = %d", *e.StartupTimeoutSec))
		}

		// Literal env values go into a sub-table emitted after every scalar key.
		if t == "stdio" && len(e.Env) > 0 {
			out = append(out, "[mcp_servers."+name+".env]")
			for _, kv := range sortedKV(e.Env) {
				out = append(out, kv.Key+" = "+tomlStr(kv.Value))
			}
		}

		out = append(out, "")
	}
	out = append(out, codexEnd)
	return pyRstrip(strings.Join(out, "\n")) + "\n"
}

// renderTrae renders the project/user Trae YAML marker block.
func renderTrae(entries []config.Server) string {
	out := []string{traeBegin, "mcp_servers:"}
	for _, e := range sortServersByName(entries) {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		t := normalizeType(e.Type)

		out = append(out, "  - name: "+yamlQuote(name))
		out = append(out, "    type: "+yamlQuote(t))

		if url := strings.TrimSpace(e.URL); url != "" {
			out = append(out, "    url: "+yamlQuote(url))
		}
		if cmd := strings.TrimSpace(e.Command); cmd != "" {
			out = append(out, "    command: "+yamlQuote(cmd))
		}
		if len(e.Args) > 0 {
			out = append(out, "    args:")
			for _, a := range e.Args {
				out = append(out, "      - "+yamlQuote(a))
			}
		}

		var headers []config.KV
		if bt := strings.TrimSpace(e.BearerTokenEnvVar); bt != "" {
			headers = []config.KV{{Key: "Authorization", Value: "Bearer ${" + bt + "}"}}
		} else {
			headers = e.Headers
		}
		if len(headers) > 0 {
			out = append(out, "    headers:")
			for _, kv := range sortedKV(headers) {
				out = append(out, "      "+kv.Key+": "+yamlQuote(kv.Value))
			}
		}

		if len(e.Env) > 0 {
			out = append(out, "    env:")
			for _, kv := range sortedKV(e.Env) {
				out = append(out, "      - key: "+yamlQuote(kv.Key))
				out = append(out, "        value: "+yamlQuote(kv.Value))
			}
		}
	}
	out = append(out, traeEnd)
	return strings.Join(out, "\n") + "\n"
}

func opencodeQ(s string) string { return pyJSONString(s, true) }

// renderOpencode renders the JSONC "mcp" member used by both project and user
// opencode targets.
func renderOpencode(entries []config.Server) string {
	out := []string{"  " + opencodeBegin, "  \"mcp\": {"}
	renderedAny := false

	for _, e := range sortServersByName(entries) {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		t := normalizeType(e.Type)

		if renderedAny {
			out[len(out)-1] += ","
		}
		out = append(out, "    "+opencodeQ(name)+": {")

		if t == "stdio" {
			cmdList := []string{}
			if cmd := strings.TrimSpace(e.Command); cmd != "" {
				cmdList = append(cmdList, cmd)
			}
			cmdList = append(cmdList, e.Args...)
			parts := make([]string, len(cmdList))
			for i, c := range cmdList {
				parts[i] = opencodeQ(c)
			}
			out = append(out, "      \"type\": \"local\",")
			out = append(out, "      \"command\": ["+strings.Join(parts, ", ")+"],")
			out = append(out, "      \"enabled\": true")

			envEntries := map[string]string{}
			for _, kv := range e.Env {
				envEntries[kv.Key] = kv.Value
			}
			for _, v := range e.EnvVars {
				if _, ok := envEntries[v]; !ok {
					envEntries[v] = "{env:" + v + "}"
				}
			}
			if len(envEntries) > 0 {
				out[len(out)-1] += ","
				out = append(out, "      \"environment\": {")
				keys := make([]string, 0, len(envEntries))
				for k := range envEntries {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				pairs := make([]string, len(keys))
				for i, k := range keys {
					pairs[i] = "        " + opencodeQ(k) + ": " + opencodeQ(envEntries[k])
				}
				out = append(out, strings.Join(pairs, ",\n"))
				out = append(out, "      }")
			}
		} else {
			url := strings.TrimSpace(e.URL)
			out = append(out, "      \"type\": \"remote\",")
			out = append(out, "      \"url\": "+opencodeQ(url)+",")
			out = append(out, "      \"enabled\": true")

			if bt := strings.TrimSpace(e.BearerTokenEnvVar); bt != "" {
				out[len(out)-1] += ","
				out = append(out, "      \"headers\": {")
				out = append(out, "        \"Authorization\": "+opencodeQ("Bearer {env:"+bt+"}"))
				out = append(out, "      },")
				out = append(out, "      \"oauth\": false")
			}
		}

		if e.StartupTimeoutSec != nil {
			out[len(out)-1] += ","
			out = append(out, fmt.Sprintf("      \"timeout\": %d", *e.StartupTimeoutSec*1000))
		}

		out = append(out, "    }")
		renderedAny = true
	}

	out = append(out, "  }", "  "+opencodeEnd)
	return strings.Join(out, "\n") + "\n"
}
