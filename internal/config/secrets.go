package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// ParseSecrets reads a flat TOML table of string values, e.g.
// browserless_token = "...". It is consumed by the MCP domain (Stage 2); the
// skills domain never needs secrets. A missing file yields an empty map.
func ParseSecrets(path string) (map[string]string, error) {
	secrets := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return secrets, nil
		}
		return nil, fmt.Errorf("%s: cannot read secrets %s: %v", Prog, path, err)
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("%s: secrets file is not valid TOML: %s: %v", Prog, path, err)
	}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s: secret \"%s\" must be a string in %s", Prog, k, path)
		}
		secrets[k] = s
	}
	return secrets, nil
}
