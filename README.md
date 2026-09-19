# agent-env

单一 Go 二进制工具，按 repo 声明环境，一条命令恢复 coding agent 的 skills 与 MCP 配置。类似 `mise`/`asdf` 管理工具版本的思路，这里管理的是 agent 的能力集：

- **skills**：哪些 skill 源安装到本 repo（project scope）或全局（user scope）
- **MCP**：哪些 MCP server 注册到本 repo 的各个 agent，以及 user 级（全局）server

一个二进制、两个域、一份 repo 级配置：

- 全局（user 级）skills + MCP 清单统一维护在本 repo 的 `config.toml`，经 chezmoi symlink 到 `~/.config/agent-env/config.toml`。
- 每个 repo 用 `.agent-env.toml` 声明自己启用的 profiles / agents。
- `config.toml` 里的 `${VAR}` 从 `~/.config/agent-env/secrets.toml`（chezmoi private，不进 repo）或环境变量解析；dry-run 对 secrets 值脱敏。

## 安装

三种方式，任选其一：

1. **下载 Release 二进制**：在 GitHub Release 页下载对应平台的 `agent-env_<os>_<arch>.tar.gz`（`darwin`/`linux` × `amd64`/`arm64`），解压得到 `agent-env` 可执行文件与 `README.md`，放进 `PATH` 即可。同时提供 `.sha256` 校验文件。
2. **`go install`**（需 Go 1.25+）：

   ```sh
   go install github.com/yanickxia/agent-env@latest
   ```

   二进制落在 `$(go env GOPATH)/bin/agent-env`。

3. **源码构建**：

   ```sh
   go build -o bin/agent-env ./cmd/agent-env
   ```

   `bin/agent-env` 是 gitignored 的构建产物。

> 版本号默认为开发版本，Release 构建通过 `-ldflags -X github.com/yanickxia/agent-env/internal/version.Version=<tag>` 注入 tag；`agent-env version` / `--version` 读取该值。

## Bootstrap

```sh
git clone <agent-env repo> ~/codes/mine/agent-env   # 1. 检出内容源头
cd ~/codes/mine/agent-env && go build -o bin/agent-env ./cmd/agent-env   # 2. 构建二进制
chezmoi apply                                        # 3. 接线 symlink + secrets + 钩子（自动同步 user 级 skills/MCP）
```

`chezmoi apply` 会：symlink `~/.local/bin/agent-env -> <repo>/bin/agent-env`、`~/.config/agent-env/config.toml -> <repo>/config.toml`、写 `~/.config/agent-env/secrets.toml`（chezmoi private），并跑 user 级同步钩子。

## 快速开始

在一个新 clone 的 repo 里：

```sh
# 1. 声明这个 repo 需要的 profiles（写入 <repo>/.agent-env.toml）
agent-env init base ark-mlops --agent codex,opencode

# 2. 一次跑两个域：先 skills，再 MCP（project scope，按 .agent-env.toml 过滤）
agent-env apply --scope project --non-interactive

# 也可以只跑单个域
agent-env skills apply --scope project --non-interactive
agent-env mcp apply --scope project --non-interactive
```

`--apply` 可以把第 1、2 步合并：`agent-env init base ark-mlops --apply`。

想先看会做什么：

```sh
agent-env dry-run --scope project          # 两个域都会打印将执行的命令/将写入的配置块
agent-env skills resolve                   # 当前 repo 最终会安装的 skills 条目
agent-env skills status                    # 当前 repo 的 stamp 状态（跳过已安装的依据）
agent-env mcp dry-run --scope project      # MCP 会写入哪些配置
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
agent-env apply    [--scope user|project|all] [--agent A]... [--profile P]... [--profiles a,b] [--non-interactive] [--skip-unchanged]
agent-env dry-run  [同上 flags]
```

- 语义：先跑 skills、再跑 MCP，**两者都执行**（即使第一个失败也继续第二个）；输出以 `=== skills ===` / `=== mcp ===` 分隔；最终 exit code = 任一非零则非零。
- `--skip-unchanged` 只传给 skills，MCP 忽略（help 中已说明）。
- `--skill` / `--name` 等域专属 flag 不在顶层定义，cobra 会将其视为未知 flag 拒绝；需要时用对应的 `skills` / `mcp` 子命令。
- `apply` 必须带 `--non-interactive`（未提供交互选择）。
- 所有过滤器 flag 都支持**重复传递**与**逗号分隔**（`--agent codex,opencode`）。

### skills

```sh
agent-env skills apply    [--scope user|project|all] [--agent A]... [--skill S]... [--skills a,b] [--profile P]... [--profiles a,b] [--non-interactive] [--skip-unchanged]
agent-env skills dry-run  [同上 flags]
agent-env skills list                     # 打印 config.toml 原文
agent-env skills profiles                 # 列出 [[installs]] 声明的 profiles（排序去重；无则 rc=1）
agent-env skills resolve  [--agent A]... [--profile P]...    # 只读：当前 repo 会安装什么
agent-env skills status   [--agent A]... [--profile P]...    # 只读：project stamp 状态
```

- `user` scope 由 chezmoi 91 钩子自动同步（`config.toml` sha256 变化触发 `skills apply --scope user --non-interactive --skip-unchanged`）。
- `project` scope 必须显式提供选择：没有 `.agent-env.toml` 也没有 `--profile` 时，显式 `--scope project` 直接报错；未过滤运行改为跳过 project 条目并提示。
- `--skip-unchanged` 靠 stamp 去重，且只用于完整运行（与 `--agent`/`--skill` 组合报错；dry-run 永不写 stamp）。project stamp 按 repo root + profiles 签名区分，user stamp 与既有状态文件格式字节兼容。
- 只读命令 `resolve`/`status` 需要 repo 配置或 `--profile`，且拒绝 `--scope` 非 project、`--skill`/`--skills`、`--skip-unchanged`；`profiles`/`list` 拒绝一切过滤器。
- `list` 只读 `[[installs]]`，忽略 `[[servers]]`。

### mcp

```sh
agent-env mcp apply       [--scope user|project|all] [--agent A]... [--name N]... [--names a,b] [--profile P]... [--profiles a,b] [--non-interactive]
agent-env mcp dry-run     [同上 flags]
agent-env mcp list                        # 打印 config.toml 原文
agent-env mcp profiles                    # 列出 [[servers]] 声明的 profiles
agent-env mcp upsert-stdin --agent AGENT  # stdin→stdout：插入/替换该 agent 的 user 级 marker 块（chezmoi modify_ 专用）
```

写入目标（marker 块替换，幂等）：

- project 级：`<repo>/.codex/config.toml`、`<repo>/.trae/traecli.yaml`、`<repo>/.opencode/opencode.jsonc`；claude/aiden 走 CLI。
- user 级：`~/.codex/config.toml`、`~/.trae/traecli.yaml`、`~/.config/opencode/opencode.jsonc`；aiden 走 CLI（user→global）；claude patch `~/.claude.json` 顶层 `mcpServers`，其余运行时键原样保留。
- user 级 codex/trae/opencode 由 chezmoi `modify_` 脚本在 apply 内直接算出最终内容：渲染 base 后 pipe 给 `agent-env mcp upsert-stdin --agent X`（与 `apply --scope user` 共享同一渲染核心，字节级一致），chezmoi 自己写文件，无外部 writer、无 drift。
- `list`/`profiles` 只读 `[[servers]]`，忽略 `[[installs]]`。

### init

```sh
agent-env init PROFILE... [--agent AGENT]... [--apply] [--dry-run]
```

创建或合并 `<git toplevel>/.agent-env.toml`（fallback `$PWD`）：

- 已存在时旧 profiles/agents 在前、CLI 新值去重追加；`mode`/`[vars]` 原样保留；原子写（同目录 temp+rename）；注释不保留。
- 至少一个 kebab-case profile；非法名拒绝、重复去重保序；`--agent` 支持逗号/重复。
- `--dry-run` 打印路径与将写入的完整内容，不落盘。
- `--apply` 写完后再执行 `agent-env skills apply --scope project --non-interactive --skip-unchanged`，透传其 exit code。

## `.agent-env.toml`

repo 根目录的声明文件（建议提交；只做选择，不能执行命令）：

```toml
profiles = ["base", "ark-mlops"]   # 必填，kebab-case 字符串数组
agents   = ["codex", "opencode"]   # 可选，收窄安装目标（与条目 agents 取交集）
mode     = "symlink"               # 可选，symlink | copy
[vars]                             # 可选预留表
team = "ark"
```

只读命令与 apply 都接受：`--profile` 覆盖 repo 配置（临时，不写回）。缺少文件 = 无选择，不报错；文件非法（缺 profiles、非 kebab、未知 key）→ rc=1。

## Profile 语义对照表

| 场景 | skills | MCP |
|---|---|---|
| 无选择（无配置无 `--profile`） | 显式 `--scope project` 硬报错；未过滤运行跳过 project 条目并计数提示 | **不过滤，全量**（存量兼容语义） |
| 有选择，条目带 profiles | project 条目交集非空才安装 | 同左（仅 project 行；user 行不受影响） |
| 有选择，条目无 profiles | project 条目跳过 | 同左，并在结束时汇总 `skipped N project servers without matching profiles` |
| repo 配置声明 agents | project 条目 agents ∩ repo agents，空则跳过 | 同左 |

> skills 与 MCP 的门禁差异是刻意的：MCP 存量配置大量 project server 没有 profiles，为保持旧行为，无选择时不做任何过滤。

## 统一配置、secrets 与脱敏

`config.toml` 一个文件两个顶层表：`[[installs]]`（skills）与 `[[servers]]`（MCP）；两个域各取所需、互相忽略。关键字段：

- `[[installs]]`：`source`(必填)、`agents`、`skills`（`["*"]`=全部）、`scope`(必填 `user`/`project`，单值)、`mode`、`profiles`、`post_install`、`installer`（仅 `skills`）、`env`。
- `[[servers]]`：`name`(必填)、`type`（`stdio`/`streamable-http`，另兼容 `sse`）、`command`、`args`、`url`、`env`、`headers`、`env_vars`、`bearer_token_env_var`、`scope`（字符串或字符串数组，去重保序）、`profiles`、`startup_timeout_sec`。

`${VAR}` 在解析期统一展开，覆盖范围：`[[servers]]` 的 `args` 元素、`url`、`env` 值、`headers` 值。解析顺序：`~/.config/agent-env/secrets.toml` 的 exact key → lowercase key → 环境变量 → 空串（静默）。

dry-run 脱敏：**来自 secrets.toml 的所有值出现即替换为 `***redacted***`**（无论出现在 CLI 命令还是渲染的 config block）；env 兜底值不脱敏（用户 shell 本就可见）。apply 写入目标文件的是真实值。

## chezmoi 集成点

| 位置 | 作用 |
|---|---|
| `dot_local/bin/symlink_agent-env` | symlink → `<repo>/bin/agent-env`（PATH 中可直接用） |
| `dot_config/agent-env/symlink_config.toml` | symlink → `<repo>/config.toml` |
| `dot_config/agent-env/private_secrets.toml` | 全部 token（chezmoi private，不进 repo） |
| `.chezmoiscripts/run_onchange_after_91_agent-env-skills.sh.tmpl` | user 级 skills 自动同步（fingerprint = `<repo>/config.toml` sha256；容错不阻塞） |
| `.chezmoiscripts/run_after_92_agent-env-mcp.sh.tmpl` | claude/aiden + `.codex` 与 `config.toml` 部署顺序收敛（每次 apply 运行） |
| `dot_codex/modify_private_config.toml.tmpl` | codex user 级 modify_（base + `agent-env mcp upsert-stdin --agent codex`） |
| `dot_trae/modify_traecli.yaml.tmpl` | trae user 级 modify_ |
| `dot_config/opencode/modify_opencode.jsonc.tmpl` | opencode user 级 modify_ |
| `dot_config/zsh/config.d/common/{skills,mcp}.zsh` | 便捷函数 `agent-skills` / `agent-mcp`（包装 `agent-env skills|mcp`） |

## 从 tag 发版

推送形如 `v1.2.3` 的 tag 会触发 `.github/workflows/release.yml`：

1. `test` job：`gofmt` 检查 + `go vet ./...` + `go test ./...`（与 `.github/workflows/ci.yml` 相同）。
2. `build` job（matrix）：交叉编译 `darwin/amd64`、`darwin/arm64`、`linux/amd64`、`linux/arm64`，版本号由 tag 经 `-ldflags -X .../internal/version.Version=${GITHUB_REF_NAME}` 注入；每个平台打 `agent-env_<os>_<arch>.tar.gz`（内含二进制 + `README.md`），并生成 `.sha256`。
3. `release` job：汇总产物，用 `softprops/action-gh-release@v2` 创建 Release（附全部 tar.gz + sha256，自动生成 release notes）。

`.github/workflows/ci.yml` 在每次 push（所有分支）与 pull_request 上跑同样的检查。注意：仓库首次 push 后工作流才会生效。

## 环境变量

| 变量 | 作用 |
|---|---|
| `AGENT_ENV_CONFIG` | 覆盖全局配置路径（canonical；默认 `~/.config/agent-env/config.toml`） |
| `AGENT_SKILLS_MANIFEST` | 兼容覆盖同一份合并配置路径 |
| `AGENT_SKILLS_STATE` | 覆盖 skills stamp 文件路径（默认 `$HOME/.local/state/` 下的 `state.tsv`，沿用历史目录名以兼容既有 stamp） |
| `AGENT_MCP_SECRETS` | 覆盖 secrets 路径（默认 `~/.config/agent-env/secrets.toml`） |
| `AGENT_ENV_REPO_CONFIG` | 覆盖 repo 配置路径（旧名 `AGENT_SKILLS_REPO_CONFIG` 静默兼容） |
| `CLAUDE_JSON` | 覆盖 `~/.claude.json`（user 级 claude MCP patch 目标） |
| `AGENT_SKILLS_SYNC_BIN` | 覆盖 `agent-env init --apply` 调用的可执行文件（默认 re-exec 自身） |

## 测试

```sh
go test ./...
```

table-driven Go 测试覆盖：manifest/repo config 校验矩阵、profile 选择/交集/agents 收窄、门禁、stamp 签名与真实状态文件字节兼容、installer 命令构造、claude symlink 特例、skip-unchanged 全流程（stub npx）、MCP `${VAR}`/脱敏、三渲染器 marker 语义、claude 保序 patch、upsert-stdin 与 apply 字节等价、aiden 命令构造、init 新建/合并/apply 透传、顶层 all-in-one 的双域执行/失败聚合/flag 透传。

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
config.toml                    # 全局配置：[[installs]] + [[servers]]，无 secrets
.github/workflows/{ci,release}.yml
go.mod / go.sum
bin/agent-env                  # 构建产物（gitignored）
```

## 设计文档

完整设计与决策记录：`~/.local/share/chezmoi/docs/agent-skills-profile-manager.md`。