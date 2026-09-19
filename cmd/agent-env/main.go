// Command agent-env manages agent skills and MCP servers from a single
// binary. It is the Go successor to the retired zsh agent-skills-sync /
// agent-mcp-sync / agent-skills-init scripts.
package main

import (
	"os"

	"github.com/yanickxia/agent-env/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
