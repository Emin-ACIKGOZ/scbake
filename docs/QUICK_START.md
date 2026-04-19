# Quick Start: Get Started in 5 Minutes

## Install & Build

```bash
git clone https://github.com/Emin-ACIKGOZ/scbake.git
cd scbake
go build -o scbake main.go
```

## Create Your First Project

```bash
./scbake new my-backend --lang go --with makefile,ci_github
cd my-backend
```

What you get:
- ✅ Go project with `go.mod`
- ✅ `Makefile` with `build`, `test`, `lint` targets
- ✅ GitHub Actions CI configuration
- ✅ `scbake.toml` manifest tracking everything applied

## Explore

```bash
# See what was created
ls -la

# Check the manifest
cat scbake.toml

# List what's in your project
../scbake list projects

# See available templates
../scbake list templates
```

## Add More Templates

```bash
# Add a linter and dev container
../scbake apply --with go_linter,devcontainer

# Check the manifest (updated!)
cat scbake.toml
```

If something goes wrong:
```bash
# Undo the last operation
cd ..
rm -rf my-backend
# Or use --dry-run to preview first:
./scbake apply --dry-run --with go_linter
```

## Create Other Projects

### Python (Spring Boot) Backend

```bash
./scbake new my-api --lang spring --with makefile,ci_github,maven_linter
cd my-api
cat pom.xml  # Spring starter generated!
```

### Svelte Frontend

```bash
./scbake new my-frontend --lang svelte --with makefile,editorconfig
cd my-frontend
npm install  # Dependencies already configured
```

### Git-only Project

```bash
./scbake new my-docs --with git,editorconfig
cd my-docs
git log  # Already has initial commit!
```

## Available Languages

| Language | Use case | Requires |
|----------|----------|----------|
| **go** | Backend, CLI, services | `go` binary |
| **spring** | Java backends, services | `curl`, `unzip`, `java` |
| **svelte** | Frontends, web apps | `npm` |

## Available Templates

| Template | What it does |
|----------|--|
| **git** | Initialize repo, first commit |
| **editorconfig** | Standard editor formatting |
| **ci_github** | GitHub Actions workflows |
| **makefile** | Universal build scripts |
| **go_linter** | golangci-lint config |
| **maven_linter** | Checkstyle for Java |
| **svelte_linter** | ESLint for Svelte |
| **devcontainer** | VS Code dev container |

## Dry-Run (Preview Without Changes)

Before applying changes, see what would happen:

```bash
./scbake apply --dry-run --with go_linter,devcontainer
# Shows what would be created, no actual changes
```

## Force Override

If scbake warns about overwrites:

```bash
./scbake apply --force --with go_linter
# Overrides existing files
```

## Extend scbake

Want to add your own language or template? See [EXTENDING.md](./EXTENDING.md).

TL;DR:
1. Fork the repo
2. Add handler to `pkg/lang/mylag/` or `pkg/templates/mytemplate/`
3. Compile: `go build -o scbake main.go`
4. Use it: `scbake new test --lang mylag`

## Troubleshooting

**"command not found"**
- Ensure required binary is installed (go, npm, curl, etc.)
- Check: `which go`, `which npm`, etc.

**"File already exists"**
- Use `--dry-run` to preview first
- Use `--force` to overwrite

**"Transaction rollback"**
- If a task fails, scbake automatically rolls back all changes
- Fix the issue and try again

## Next Steps

- **Read the README**: Full feature overview
- **Explore EXTENDING.md**: Add custom handlers
- **Check ARCHITECTURE.md**: Understand how scbake works
- **Report issues**: Found a bug? Open an issue on GitHub

---

**That's it!** You're now ready to scaffold projects with scbake.
