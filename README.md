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

## 升级

日常升级用 `agent-env update`（`install.zsh` 仍是首次 bootstrap 安装）：

```sh
agent-env update                 # 安装最新 release，原子替换自身
agent-env update --version v0.3.0
```

- 已是最新 → `already up to date (vX.Y.Z)`（exit 0）；成功 → `updated vOLD → vNEW`。
- 下载 release 的 `agent-env_<os>_<arch>.tar.gz` 并校验 sha256，失败拒绝安装；替换失败会提示改用 `zsh install.zsh install`。
- `dev` 构建也可运行 update（会用最新 release 覆盖 dev 构建并注明）。

**启动自动检查**（异步、best-effort）：发现新版本时在 **stderr** 打一行
`agent-env: vX.Y.Z is available (current: vOLD); run 'agent-env update' to upgrade`。
只写 stderr，**绝不污染 stdout**（`agent-env mcp upsert-stdin` 的管道输出保持纯净）。检查通过
`releases/latest` 的 302 `Location` 解析版本，不调用 GitHub API。

频控与缓存：

- 缓存：`$XDG_CACHE_HOME/agent-env/update-check.json`（默认 `~/.cache/agent-env/update-check.json`），字段 `{latest, checked_at, notified_at}`。
- TTL 24h：24h 内直接用缓存不发请求；提示也 24h 最多一次（同版本），chezmoi apply 多次调用 agent-env 不会刷屏。
- 跳过：`dev`（非 semver）构建、`AGENT_ENV_NO_UPDATE_CHECK=1|true`、以及 `agent-env update` 命令自身。
- 网络失败/超时（2s）→ 全静默。

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
agent-env init PROFILE... [--name N]... [--agent A]... [--apply] [--dry-run]
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

#### 分发机制（npx skills）

skills 的实际安装委托给 [vercel-labs/skills](https://github.com/vercel-labs/skills) 的 `npx skills`：agent-env 只按 repo/profile 选出条目、拼出 `npx --yes skills add ...` 命令并保证幂等，不自己实现下载与落盘。`source` 接受 GitHub 简写（`owner/repo`）、完整 URL、GitLab/Azure 等任意 git URL（含 `git@...`）、本地路径，以及直接下载 URL。

- **支持哪些 agent**：`agents` 中的名字原样传给 `npx skills add ... -a <agent>`，可选集合就是 vercel-labs/skills 的 available-agents（80+，含 `claude-code`、`codex`、`opencode`、`trae`、`trae-cn`、`pi` 等；`*` 表示全部）。例外：`aiden` 不走 npx，改由 `aiden skills add` 安装。当前 `config.toml` 实际只声明 `claude-code` / `codex` / `opencode`。
- **装到哪里**：全局（profiles 含 `global`）追加 `-g`，装进各 agent 的全局 skills 目录（`claude-code` → `~/.claude/skills/`、`codex` → `~/.codex/skills/`、`opencode` → `~/.config/opencode/skills/`、`pi` → `~/.pi/agent/skills/` 等，逐 agent 不同）；repo 级（profiles 不含 `global`）不加 `-g`，在 repo 根目录安装（`claude-code` → `.claude/skills/`，`codex` / `opencode` → `.agents/skills/`）。
- **什么模式**：默认 symlink——各 agent 目录 symlink 到统一 skills store；`mode = "copy"` 追加 `--copy`，为每个 agent 复制独立副本。
- **特例**：当 `~/.claude/skills` 本身已 symlink 到统一 skills store 时，运行时跳过 `-a claude-code` 并打印提示（claude 直接读 store）。
- 仅 skills 域使用的字段：`installer`（当前仅 `skills`）、`post_install` 依赖钩子；`skills = ["*"]` 表示源内全部 skill。

### mcp

```sh
agent-env mcp apply       [--agent A]... [--name N]... [--names a,b] [--profile P]... [--profiles a,b] [--non-interactive]
agent-env mcp dry-run     [同上 flags]
agent-env mcp list                        # 打印 config.toml 原文
agent-env mcp profiles                    # 列出可声明的 profiles（排除 global）
agent-env mcp upsert-stdin --agent AGENT  # stdin→stdout：插入/替换该 agent 的全局 marker 块（chezmoi modify_ 专用）
```

写入目标（幂等重新 apply；marker 块替换或整体管理，重复运行结果一致）：

| `agents` 中的名字 | 全局目标（profiles 含 `global`） | repo 级目标 | 格式 |
|---|---|---|---|
| `codex` | `~/.codex/config.toml` | `<repo>/.codex/config.toml` | TOML `[mcp_servers.*]` marker 块 |
| `claude-code` | patch `~/.claude.json` 顶层 `mcpServers`（其余运行时键原样保留） | `claude mcp add` CLI（repo 级） | JSON |
| `trae` / `trae-cn` | `~/.trae/traecli.yaml` | `<repo>/.trae/traecli.yaml` | YAML `mcp_servers` marker 块 |
| `opencode` | `~/.config/opencode/opencode.jsonc` | `<repo>/.opencode/opencode.jsonc` | JSONC `"mcp"` 对象（`local`/`remote`） |
| `aiden` | `aiden mcp add -s global` CLI | `aiden mcp add -s project` CLI | CLI |
| `pi` | `~/.pi/agent/mcp.json` | `<repo>/.pi/mcp.json` | JSON `mcpServers` |
| `omp`（oh-my-pi） | `~/.omp/agent/mcp.json` | `<repo>/.omp/mcp.json` | JSON `mcpServers` |

> MCP 域的 claude 规范名是 `claude-code`（旧写法 `claude` 仍被接受并自动归一）；`trae-cn` 与 `trae` 共用同一份 `traecli.yaml`。

- 全局 codex/trae/opencode 由 chezmoi `modify_` 脚本在 apply 内直接算出最终内容：渲染 base 后 pipe 给 `agent-env mcp upsert-stdin --agent X`（与全局 apply 共享同一渲染核心，字节级一致），chezmoi 自己写文件，无外部 writer、无 drift。
- `pi` / `omp` 没有 chezmoi `modify_` 模板，全局由 `agent-env mcp apply` 直接写（chezmoi 92 钩子每次 apply 都会跑）：整体管理顶层 `mcpServers` 键，其余顶层键保留；目标文件不存在且无条目时不创建空文件。
- `list`/`profiles` 只读 `[[servers]]`，忽略 `[[installs]]`。

能力差异：

- `env_vars`（环境变量引用透传）：仅 `codex` / `opencode` 支持；其余（`claude-code` / `trae` / `aiden` / `pi` / `omp`）忽略。
- `startup_timeout_sec`：`codex` 原样写 `startup_timeout_sec`，`opencode` 转成毫秒 `timeout`；其余忽略。
- `bearer_token_env_var`：`codex` / `trae` / `opencode` 保留 env 引用（分别写 `bearer_token_env_var`、`Authorization: Bearer ${VAR}`、`Bearer {env:VAR}`）；`claude-code` / `aiden` / `pi` / `omp` 在 apply 时解析为明文。
- 更新方式：所有 agent 统一为幂等重新 apply（`agent-env mcp apply`），无 watch / 热重载；运行中的 agent 需重启才生效。

### init

```sh
agent-env init PROFILE... [--name N]... [--agent AGENT]... [--apply] [--dry-run]
```

创建或合并 `<git toplevel>/.agent-env.toml`（fallback `$PWD`）：

- 已存在时旧 profiles/agents 在前、CLI 新值去重追加；`mode`/`[vars]` 原样保留；原子写（同目录 temp+rename）；注释不保留。
- 至少一个 PROFILE 或 `--name`；非法名与保留字 `global` 均拒绝；`--name`/`--agent` 支持逗号/重复。
- `--dry-run` 打印路径与将写入的完整内容，不落盘。
- `--apply` 写完后再执行 `agent-env skills apply --non-interactive --skip-unchanged`，透传其 exit code。

## `.agent-env.toml`

repo 根目录的声明文件（建议提交；只做选择，不能执行命令）：

```toml
profiles = ["base", "ark-mlops"]   # 可选，kebab-case 字符串数组；不可写 global
names    = ["clickup", "playwright"]  # 可选，按条目 name 点名（跨域匹配）
agents   = ["codex", "opencode"]   # 可选，收窄 repo 级条目的 agents
mode     = "symlink"               # 可选，symlink | copy
[vars]                             # 可选预留表
team = "ark"
```

只读命令与 apply 都接受：`--profile` 覆盖 repo 配置的 profiles（临时，不写回）。缺少文件 = 无选择，不报错；`profiles`/`names` 两者皆无 = 无选择（合法）；文件非法（非 kebab、含 `global`、未知 key）→ rc=1。

## 层级配置（mise 式发现）

`agent-env` 从**当前目录**逐级向上走到 `/`，收集沿途所有 `.agent-env.toml` 并合并（就近优先）。这样可把共享选择放在父目录，子 repo 免重复声明：

```text
~/codes/bytedance/.agent-env.toml                  # profiles = ["ark-mlops"]（团队公共）
~/codes/bytedance/aml/model-proxy/                  # 子 repo 自动继承，无需重复声明
~/codes/bytedance/aml/model-proxy/.agent-env.toml   # 可选：profiles = ["base"]（子层追加）
```

- 发现范围自然包含 `$HOME` 级与 git repo root 级；某层文件存在但解析/校验非法 → 报错退出，消息带该文件完整路径。
- `AGENT_ENV_REPO_CONFIG`（或兼容别名 `AGENT_SKILLS_REPO_CONFIG`）设置时 = **单文件模式**，不向上查找（既有用法完全兼容）。
- 安装目标与 stamp 的 `repo_root` key 仍锚定 **git toplevel**（fallback cwd）——不会把 project 配置写到父目录。

合并规则（多层叠加，就近最深优先）：

| 字段 | 规则 |
|---|---|
| `profiles` | **并集**，就近优先去重保序（父层给公共选择，子层追加特有） |
| `agents` | **就近声明胜出**；都没声明 → 空（用条目自身 agents） |
| `mode` | 就近声明胜出 |
| `vars` | 按 key 合并，就近覆盖 |

有效 profiles（去重排序后）进 stamp 签名：父层配置变化会触发一次重装，属预期。`resolve`/`status` 会先列出发现的配置层（就近在前）与合并后的 profiles/agents；`init` 仍只写/合并 git toplevel（fallback cwd）那一层文件，不跨层合并。

## Profile 与 name 语义

manifest 条目有三态，由 `profiles` 是否包含保留关键字 `global` 决定：

- `profiles = ["global"]` → **全局条目**：无条件安装，不受 repo 选择影响。可与其他 tag 混写（如 `["global", "base"]`），global 决定层级，其余 tag 仅分类。
- `profiles = ["bytedance"]`（不含 global）→ **组条目（repo 级）**：按 repo 选择的 profiles 交集门禁。
- **无 `profiles` 字段（或空数组）→ 游离条目**：不属于任何组，任何 profiles 选择都不命中；**只有被 repo 配置 `names` 点名才安装**。

`names` 是 repo 侧的**个体选择器**（`.agent-env.toml` 的 `names = [...]`）：按条目 `name` 跨域匹配——一条 `name = "clickup"` 同时命中同名的 skill 与 MCP server（`[[installs]]` 与 `[[servers]]` 同名允许，这是有意设计）。name 点名可越过 profile 分组；全局条目不受 names 影响。

```toml
# .agent-env.toml
profiles = ["bytedance"]        # 组选择
names    = ["playwright"]       # 个体点名（越过分组）
```

| 场景 | 全局条目 | 组条目 | 游离条目 |
|---|---|---|---|
| 无选择（无 `.agent-env.toml` 无 `--profile`） | 照装 | 跳过并汇总提示 | 跳过并汇总提示 |
| 选择的 profiles 与条目 profiles 有交集 | 照装 | 安装 | — |
| 条目 name ∈ 选择的 names | 照装 | 安装（越过分组） | 安装 |
| 有选择但都不命中 | 照装 | 跳过 | 跳过 |
| repo 配置声明 agents | 不受影响 | agents ∩ repo agents（含 name 选中的条目），空则跳过 | 同左 |

> 两域门禁统一（skills 与 MCP 行为一致）。`global` 仍不可出现在 profiles/names 中。MCP `--name` CLI 过滤器是在“已活跃集合”内的二次收窄，与 repo 配置的 `names` 选择器是两层语义。

## 统一配置、secrets 与脱敏

`config.toml` 一个文件两个顶层表：`[[installs]]`（skills）与 `[[servers]]`（MCP）；两个域各取所需、互相忽略。关键字段：

- `[[installs]]`：`source`(必填)、`name`(可选，kebab-case，跨 installs 唯一；供 repo `names` 点名)、`agents`、`skills`（`["*"]`=全部）、`profiles`(可选；缺省/空 = 游离)、`mode`、`post_install`、`installer`（仅 `skills`）、`env`。
- `[[servers]]`：`name`(必填)、`type`（`stdio`/`streamable-http`，另兼容 `sse`）、`command`、`args`、`url`、`env`、`headers`、`env_vars`、`bearer_token_env_var`、`profiles`(可选；缺省/空 = 游离)、`startup_timeout_sec`。

**agents**：本仓库统一使用 `["claude-code", "codex", "opencode"]`（`[[installs]]` 与 `[[servers]]` 都是）。MCP 域的 canonical 名是 `claude-code`；历史写法 `claude` 仍作为别名被接受，并在解析期归一化为 `claude-code`（`--agent claude` 与 `--agent claude-code` 等价）。trae / aiden / pi 的 writer 代码仍保留并受支持，但当前配置不再使用（休眠状态）；需要时把对应名字加进条目 `agents` 即可。

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
| `AGENT_ENV_NO_UPDATE_CHECK` | `1`/`true` 关闭启动升级检查 |
| `AGENT_ENV_RELEASE_BASE` | 覆盖 release base URL（默认 `https://github.com/yanickxia/agent-env`；internal/testing 用） |

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