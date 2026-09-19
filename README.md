# agent-env

单一 Go 二进制工具，按 repo 声明环境，一条命令恢复 coding agent 的 skills 与 MCP 配置。类似 `mise`/`asdf` 管理工具版本的思路，这里管理的是 agent 的能力集：

- **skills**：哪些 skill 源安装到本 repo 或全局
- **MCP**：哪些 MCP server 注册到本 repo 的各个 agent，以及全局 server

一个二进制、两个域、一份配置：

- 全局配置 `config.toml`（`[[installs]]` + `[[servers]]`）由 chezmoi 直接管理为真实文件 `~/.config/agent-env/config.toml`；本 repo 只保留样例 `config.example.toml`。
- 每个 repo 用 `.agent-env.toml` 声明自己启用的 profiles / agents。
- `config.toml` 里的 `${VAR}` 从 `~/.config/agent-env/secrets.toml`（chezmoi private，不进 repo）或环境变量解析；dry-run 对 secrets 值脱敏。

## 安装

推荐用一键安装脚本，它下载对应平台的 Release tarball、校验 sha256 并安装到 `~/.local/bin`（二进制由安装器拥有）：

```sh
curl -fsSL https://raw.githubusercontent.com/yanickxia/agent-env/master/install.zsh | zsh
```

其他方式：

1. **clone 后本地安装**：

   ```sh
   git clone https://github.com/yanickxia/agent-env ~/codes/mine/agent-env
   cd ~/codes/mine/agent-env && zsh install.zsh install
   ```

   指定版本/目录：`zsh install.zsh install --version v0.1.0 --bin-dir /tmp/bin`。脚本只依赖 `curl`/`tar`/`shasum`，支持 `darwin`/`linux` × `amd64`/`arm64`。

2. **`go install`**（需 Go 1.25+）：

   ```sh
   go install github.com/yanickxia/agent-env@latest
   ```

3. **手动下载 Release / 源码构建**：
   - GitHub Release 页下载 `agent-env_<os>_<arch>.tar.gz`（内含二进制与 `README.md`），并附 `.sha256` 校验文件。
   - 源码构建：`go build -o bin/agent-env ./cmd/agent-env`（gitignored 产物）。

**卸载**：

```sh
zsh install.zsh uninstall                 # 默认移除 ~/.local/bin/agent-env
zsh install.zsh uninstall --bin-dir DIR   # 或指定目录
```

## Bootstrap

```sh
# 1. 安装二进制（安装器拥有 ~/.local/bin/agent-env）
curl -fsSL https://raw.githubusercontent.com/yanickxia/agent-env/master/install.zsh | zsh

# 2. 检出内容源头，接线配置与钩子
git clone https://github.com/yanickxia/agent-env ~/codes/mine/agent-env
chezmoi apply
```

`chezmoi apply` 会：生成真实文件 `~/.config/agent-env/config.toml`（源：chezmoi `dot_config/agent-env/config.toml`）、写 `~/.config/agent-env/secrets.toml`（chezmoi private），并跑全局同步钩子。二进制 `~/.local/bin/agent-env` 由安装器（`install.zsh`）拥有；91/92 钩子按 PATH → `~/.local/bin/agent-env` 查找它。

## 快速开始

在一个新 clone 的 repo 里：

```sh
# 1. 声明这个 repo 需要的 profiles（写入 <repo>/.agent-env.toml）
agent-env init base ark-mlops --agent codex,opencode

# 2. 一次跑两个域：先 skills，再 MCP（当前上下文装"全局 + 已选 repo 级"）
agent-env apply --non-interactive

# 也可以只跑单个域
agent-env skills apply --non-interactive
agent-env mcp apply --non-interactive
```

`--apply` 可以把第 1、2 步合并：`agent-env init base ark-mlops --apply`。

想先看会做什么：

```sh
agent-env dry-run                          # 两个域都会打印将执行的命令/将写入的配置块
agent-env skills resolve                   # 当前 repo 最终会安装的 repo 级 skills 条目
agent-env skills status                    # 当前 repo 的 stamp 状态
agent-env mcp dry-run                      # MCP 会写入哪些配置
```

## 命令树

```text
agent-env [--version|version]                 # 无参数打印 help
agent-env apply    [...]                      # all-in-one：先 skills 再 MCP
agent-env dry-run  [...]                      # all-in-one dry-run
agent-env skills ...                          # skills 域
agent-env mcp ...                             # MCP 域
agent-env init PROFILE... [--agent A]... [--apply] [--dry-run]
```

顶层 `apply` / `dry-run` 只接受两域**共享**的 flags：

```sh
agent-env apply    [--agent A]... [--profile P]... [--profiles a,b] [--non-interactive] [--skip-unchanged]
agent-env dry-run  [同上 flags]
```

- 语义：先跑 skills、再跑 MCP，**两者都执行**（即使第一个失败也继续第二个）；输出以 `=== skills ===` / `=== mcp ===` 分隔；最终 exit code = 任一非零则非零。
- `--skip-unchanged` 只传给 skills，MCP 忽略（help 中已说明）。
- `--skill` / `--name` 等域专属 flag 不在顶层定义，cobra 会将其视为未知 flag 拒绝；需要时用对应的 `skills` / `mcp` 子命令。
- `apply` 必须带 `--non-interactive`（未提供交互选择）。
- 所有过滤器 flag 都支持**重复传递**与**逗号分隔**（`--agent codex,opencode`）。

### skills

```sh
agent-env skills apply    [--agent A]... [--skill S]... [--skills a,b] [--profile P]... [--profiles a,b] [--non-interactive] [--skip-unchanged]
agent-env skills dry-run  [同上 flags]
agent-env skills list                     # 打印 config.toml 原文
agent-env skills profiles                 # 列出可声明的 profiles（排序去重，排除保留字 global；无则 rc=1）
agent-env skills resolve  [--agent A]... [--profile P]...    # 只读：当前 repo 会安装的 repo 级条目
agent-env skills status   [--agent A]... [--profile P]...    # 只读：repo 级 project stamp 状态
```

- 全局条目（profiles 含 `global`）由 chezmoi 91 钩子自动同步（全局 config 变化触发 `cd $HOME && agent-env skills apply --non-interactive --skip-unchanged`）。
- repo 级条目必须显式提供选择：没有 `.agent-env.toml` 也没有 `--profile` 时跳过并提示；有选择时按 `条目 profiles ∩ 选择 ≠ ∅` 门禁。
- `--skip-unchanged` 靠 stamp 去重，且只用于完整运行（与 `--agent`/`--skill` 组合报错；dry-run 永不写 stamp）。
- 只读命令 `resolve`/`status` 需要 repo 配置或 `--profile`，且拒绝 `--skill`/`--skills`、`--skip-unchanged`；`profiles`/`list` 拒绝一切过滤器。
- `list` 只读 `[[installs]]`，忽略 `[[servers]]`。

### mcp

```sh
agent-env mcp apply       [--agent A]... [--name N]... [--names a,b] [--profile P]... [--profiles a,b] [--non-interactive]
agent-env mcp dry-run     [同上 flags]
agent-env mcp list                        # 打印 config.toml 原文
agent-env mcp profiles                    # 列出可声明的 profiles（排除 global）
agent-env mcp upsert-stdin --agent AGENT  # stdin→stdout：插入/替换该 agent 的全局 marker 块（chezmoi modify_ 专用）
```

写入目标（marker 块替换，幂等）：

- repo 级（profiles 不含 global）：`<repo>/.codex/config.toml`、`<repo>/.trae/traecli.yaml`、`<repo>/.opencode/opencode.jsonc`；claude/aiden 走 CLI。
- 全局（profiles 含 global）：`~/.codex/config.toml`、`~/.trae/traecli.yaml`、`~/.config/opencode/opencode.jsonc`；aiden 走 CLI（global）；claude patch `~/.claude.json` 顶层 `mcpServers`，其余运行时键原样保留。
- 全局 codex/trae/opencode 由 chezmoi `modify_` 脚本在 apply 内直接算出最终内容：渲染 base 后 pipe 给 `agent-env mcp upsert-stdin --agent X`（与全局 apply 共享同一渲染核心，字节级一致），chezmoi 自己写文件，无外部 writer、无 drift。
- `list`/`profiles` 只读 `[[servers]]`，忽略 `[[installs]]`。

### init

```sh
agent-env init PROFILE... [--agent AGENT]... [--apply] [--dry-run]
```

创建或合并 `<git toplevel>/.agent-env.toml`（fallback `$PWD`）：

- 已存在时旧 profiles/agents 在前、CLI 新值去重追加；`mode`/`[vars]` 原样保留；原子写（同目录 temp+rename）；注释不保留。
- 至少一个 kebab-case profile；非法名与保留字 `global` 均拒绝；`--agent` 支持逗号/重复。
- `--dry-run` 打印路径与将写入的完整内容，不落盘。
- `--apply` 写完后再执行 `agent-env skills apply --non-interactive --skip-unchanged`，透传其 exit code。

## `.agent-env.toml`

repo 根目录的声明文件（建议提交；只做选择，不能执行命令）：

```toml
profiles = ["base", "ark-mlops"]   # 必填，kebab-case 字符串数组；不可写 global
agents   = ["codex", "opencode"]   # 可选，收窄 repo 级条目的 agents
mode     = "symlink"               # 可选，symlink | copy
[vars]                             # 可选预留表
team = "ark"
```

只读命令与 apply 都接受：`--profile` 覆盖 repo 配置（临时，不写回）。缺少文件 = 无选择，不报错；文件非法（缺 profiles、非 kebab、含 `global`、未知 key）→ rc=1。

## Profile 语义

`profiles` 是每个 manifest 条目的必填字段，保留关键字 `global` 决定层级：

- `profiles = ["global"]` → **全局条目**：无条件安装，不受 repo 选择影响。可与其他 tag 混写（如 `["global", "base"]`），global 决定层级，其余 tag 仅分类。
- `profiles = ["base", ...]`（不含 global）→ **repo 级条目**：按选择门禁。
- `global` 是保留字，不能出现在 `.agent-env.toml` 或 `--profile` 中（不可选）。

| 场景 | 全局条目 | repo 级条目 |
|---|---|---|
| 无选择（无 `.agent-env.toml` 无 `--profile`） | 照装 | 跳过并汇总提示 |
| 有选择，条目 profiles 与选择有交集 | 照装 | 安装 |
| 有选择，无交集 | 照装 | 跳过 |
| repo 配置声明 agents | 不受影响 | agents ∩ repo agents，空则跳过 |

> 两域门禁已统一（skills 与 MCP 行为一致）。

## 统一配置、secrets 与脱敏

`config.toml` 一个文件两个顶层表：`[[installs]]`（skills）与 `[[servers]]`（MCP）；两个域各取所需、互相忽略。关键字段：

- `[[installs]]`：`source`(必填)、`agents`、`skills`（`["*"]`=全部）、`profiles`(必填非空)、`mode`、`post_install`、`installer`（仅 `skills`）、`env`。
- `[[servers]]`：`name`(必填)、`type`（`stdio`/`streamable-http`，另兼容 `sse`）、`command`、`args`、`url`、`env`、`headers`、`env_vars`、`bearer_token_env_var`、`profiles`(必填非空)、`startup_timeout_sec`。

`${VAR}` 在解析期统一展开，覆盖范围：`[[servers]]` 的 `args` 元素、`url`、`env` 值、`headers` 值。解析顺序：`~/.config/agent-env/secrets.toml` 的 exact key → lowercase key → 环境变量 → 空串（静默）。

dry-run 脱敏：**来自 secrets.toml 的所有值出现即替换为 `***redacted***`**（无论出现在 CLI 命令还是渲染的 config block）；env 兜底值不脱敏。apply 写入目标文件的是真实值。

## chezmoi 集成点

| 位置 | 作用 |
|---|---|
| `dot_config/agent-env/config.toml` | 全局配置源（真实文件，非模板）→ `~/.config/agent-env/config.toml` |
| `dot_config/agent-env/private_secrets.toml` | 全部 token（chezmoi private，不进 repo） |
| `~/.local/bin/agent-env` | 由 `install.zsh` 安装并拥有；91/92 钩子按 PATH → `~/.local/bin/agent-env` 查找 |
| `.chezmoiscripts/run_onchange_after_91_agent-env-skills.sh.tmpl` | 全局 skills 自动同步（fingerprint = `dot_config/agent-env/config.toml` sha256；容错不阻塞） |
| `.chezmoiscripts/run_after_92_agent-env-mcp.sh.tmpl` | claude/aiden + 配置顺序收敛（每次 apply 运行） |
| `dot_codex/modify_private_config.toml.tmpl` | codex 全局 modify_（base + `agent-env mcp upsert-stdin --agent codex`） |
| `dot_trae/modify_traecli.yaml.tmpl` | trae 全局 modify_ |
| `dot_config/opencode/modify_opencode.jsonc.tmpl` | opencode 全局 modify_ |
| `dot_config/1mcp/mcp.json.tmpl`、`dot_pi/agent/mcp.json.tmpl` | 读全局配置的 `[[servers]]`，仅渲染 profiles 含 `global` 的条目并解析 `${VAR}` |
| `dot_config/zsh/config.d/common/{skills,mcp}.zsh` | 便捷函数 `agent-skills` / `agent-mcp`（包装 `agent-env skills\|mcp`） |

## 从 tag 发版

推送形如 `v1.2.3` 的 tag 会触发 `.github/workflows/release.yml`：

1. `test` job：`gofmt` 检查 + `go vet ./...` + `go test ./...`（与 `.github/workflows/ci.yml` 相同）。
2. `build` job（matrix）：交叉编译 `darwin/amd64`、`darwin/arm64`、`linux/amd64`、`linux/arm64`，版本号由 tag 经 `-ldflags -X .../internal/version.Version=${GITHUB_REF_NAME}` 注入；每个平台打 `agent-env_<os>_<arch>.tar.gz`（内含二进制 + `README.md`），并生成 `.sha256`。
3. `release` job：汇总产物，用 `softprops/action-gh-release@v2` 创建 Release。

这些 Release 产物正是 `install.zsh` 消费的对象，因此发版后安装脚本无需改动即可安装新版本。

`.github/workflows/ci.yml` 在每次 push（所有分支）与 pull_request 上跑同样的检查。

## 环境变量

| 变量 | 作用 |
|---|---|
| `AGENT_ENV_CONFIG` | 覆盖全局配置路径（canonical；默认 `~/.config/agent-env/config.toml`） |
| `AGENT_SKILLS_MANIFEST` | 兼容覆盖同一份合并配置路径 |
| `AGENT_SKILLS_STATE` | 覆盖 skills stamp 文件路径（默认 `$HOME/.local/state/` 下的 `state.tsv`） |
| `AGENT_MCP_SECRETS` | 覆盖 secrets 路径（默认 `~/.config/agent-env/secrets.toml`） |
| `AGENT_ENV_REPO_CONFIG` | 覆盖 repo 配置路径（旧名 `AGENT_SKILLS_REPO_CONFIG` 静默兼容） |
| `CLAUDE_JSON` | 覆盖 `~/.claude.json`（全局 claude MCP patch 目标） |
| `AGENT_SKILLS_SYNC_BIN` | 覆盖 `agent-env init --apply` 调用的可执行文件（默认 re-exec 自身） |

## 测试

```sh
go test ./...
```

table-driven Go 测试覆盖：manifest/repo config 校验矩阵（含 global 保留字、scope 字段拒绝）、全局条目无选择照装、repo 级无选择两域都跳过、profile 选择/交集/agents 收窄、stamp 签名与真实状态文件字节兼容、installer 命令构造、claude symlink 特例、skip-unchanged 全流程、MCP `${VAR}`/脱敏、三渲染器 marker 语义、claude 保序 patch、upsert-stdin 与 apply 字节等价、aiden 命令构造、init 新建/合并/apply 透传、顶层 all-in-one 双域执行/失败聚合/flag 透传。

## 文件布局

```text
cmd/agent-env/main.go          # 入口
internal/cli/                  # cobra 命令树（含顶层 apply/dry-run）
internal/config/               # 统一配置 + secrets + repo config 解析
internal/skills/               # skills 域
internal/mcp/                  # MCP 域（渲染器 / claude patch / upsert-stdin）
internal/repoinit/             # agent-env init
internal/stamp/                # stamp 读写 + 签名
internal/version/              # 版本变量（ldflags 注入）
config.example.toml            # 全局配置样例（真实部署由 chezmoi 管理）
install.zsh                    # 安装器
.github/workflows/{ci,release}.yml
go.mod / go.sum
bin/agent-env                  # 构建产物（gitignored）
```

## 设计文档

完整设计与决策记录：`~/.local/share/chezmoi/docs/agent-skills-profile-manager.md`。