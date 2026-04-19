# scbake Extension Examples

This directory contains example handlers for extending scbake. Use these as templates for your own extensions.

## Rust Language Handler

A complete, working example of adding a new language to scbake.

### Contents

- `rust.go` - The handler implementation (70 LOC)
- `templates/Cargo.toml.tpl` - Cargo configuration template
- `templates/main.rs.tpl` - Hello world main.rs
- `templates/gitignore.tpl` - Rust .gitignore

### How to Use

1. **Copy the handler to your scbake fork**:
   ```bash
   git clone https://github.com/Emin-ACIKGOZ/scbake.git
   cp -r rust-handler/* scbake/pkg/lang/rust/
   ```

2. **Register the handler** in `scbake/pkg/lang/registry.go`:
   ```go
   import "scbake/pkg/lang/rust"
   
   func init() {
       Register("rust", &rust.Handler{})
   }
   ```

3. **Build scbake**:
   ```bash
   cd scbake
   go build -o scbake main.go
   ```

4. **Test the handler**:
   ```bash
   ./scbake new my-app --lang rust
   cd my-app
   cat Cargo.toml  # Verify it was created
   ```

### What This Example Teaches

- ✅ How to implement the Handler interface
- ✅ How to use priority sequences
- ✅ How to create tasks using CreateDirectoryTask and CreateTemplateTask
- ✅ How to embed templates with Go's `embed` package
- ✅ How to handle template variable substitution

### Customize for Your Use Case

**To create a Python handler**: Copy this structure and:
1. Rename `rust.go` to `python.go`
2. Create `python/python.go` in `pkg/lang/`
3. Update templates (requirements.txt, setup.py, pyproject.toml, etc.)
4. Register in registry

**To create a Linter template**: Same approach, but:
1. Use `pkg/templates/python_linter/` instead
2. Use priority band `PrioLinter` (1200-1399) instead of `PrioLangSetup`
3. Include linter-specific config files

---

## Best Practices Demonstrated

1. **Clear error handling** - Returns meaningful errors
2. **Good task descriptions** - Shown in CLI output
3. **Idempotency** - Running the handler twice is safe
4. **Template embedding** - Templates bundled with binary
5. **Priority ordering** - Tasks execute in correct order (directories first, then files)

---

## More Examples

More language/template examples coming soon:
- Python language handler
- Node.js language handler
- Python linter template
- Rust linter template

Feel free to contribute examples via GitHub!

---

## Need Help?

See [EXTENDING.md](../EXTENDING.md) for a detailed guide to extending scbake.
