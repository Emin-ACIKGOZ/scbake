# Contributing to {{ if .Projects }}{{ (index .Projects 0).Name }}{{ else }}this project{{ end }}

Thank you for your interest in contributing! We want to make it as easy as possible to contribute to this project.

## How to Contribute

1.  **Report Bugs**: If you find a bug, please search existing issues to see if it has already been reported. If not, open a new issue with a clear description and steps to reproduce.
2.  **Suggest Features**: We love hearing new ideas! Please open an issue to discuss any feature suggestions.
3.  **Submit Pull Requests**:
    *   Fork the repository.
    *   Create a new branch for your changes.
    *   Ensure your code follows the project's style and passes all tests.
    *   Submit a pull request with a clear description of your changes.

## Development Setup

The project uses `scbake` for scaffolding and environment management.

```bash
# Apply development templates
scbake apply --with makefile,editorconfig
```

## Code of Conduct

Please note that this project is released with a Contributor Code of Conduct. By participating in this project you agree to abide by its terms.
