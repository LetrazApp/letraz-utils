# LaTeX Resume Themes

This directory contains LaTeX template files for different resume themes.

## Available Themes

- **default.tex**: The default resume theme with a clean, professional layout

## Adding New Themes

To add a new theme:

1. Create a new `.tex` file in this directory (e.g., `modern.tex`, `elegant.tex`)
2. Write your LaTeX template using Go template syntax for variable substitution
3. Update the `getThemeTemplate` function in `engine.go` to handle your new theme name
4. Use the same template variables as the default theme:
   - `.Name`, `.Address`, `.Email`, `.Phone`, `.Website`, `.Profile`
   - `.Sections` array with `.Kind`, `.Education`, `.Experience`, `.Skills`, `.Project`, `.Certification`

## Template Variables

The template system provides these helper functions:
- `escape`: Escapes LaTeX special characters
- `escJoin`: Escapes and joins an array of strings with a separator
- `printf`: Standard Go template printf function

## Example Usage

```latex
\section{Profile}
    \begin{onecolentry}
        {{ escape .Profile }}
    \end{onecolentry}

{{- range .Sections }}
    {{- if eq .Kind "Skills" }}
        {{- range $category, $skills := .Skills.Categories }}
            \textbf{ {{- escape $category -}} :} {{ escJoin $skills ", " }}
        {{- end }}
    {{- end }}
{{- end }}
```
