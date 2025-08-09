package latex

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"letraz-utils/pkg/models"
)

// Engine renders resume data into LaTeX using named themes
type Engine struct {
}

func NewEngine() *Engine { return &Engine{} }

// Render takes a BaseResume and theme name, and returns LaTeX content as string
func (e *Engine) Render(resume models.BaseResume, theme string) (string, error) {
	tstr, err := getThemeTemplate(theme)
	if err != nil {
		return "", err
	}

	// Prepare view-model
	vm := buildViewModel(resume)

	// Parse and render template
	funcMap := template.FuncMap{
		"escape":  escapeLaTeX,
		"join":    strings.Join,
		"escJoin": escJoin,
	}
	tmpl, err := template.New("resume").Funcs(funcMap).Parse(tstr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vm); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// ===== Theme selection =====

const DefaultTheme = "DEFAULT_THEME"

func getThemeTemplate(theme string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(theme)) {
	case "", DefaultTheme:
		return defaultThemeTemplate, nil
	default:
		return "", fmt.Errorf("unknown theme: %s", theme)
	}
}

// ===== View model and helpers =====

type ExperienceVM struct {
	Period     string
	Title      string
	Company    string
	City       string
	Country    string
	Highlights []string
}

type EducationVM struct {
	Period      string
	Institution string
	Degree      string
	Highlights  []string
}

type ProjectVM struct {
	Period     string
	Name       string
	Category   string
	Role       string
	Github     string
	Live       string
	Skills     []string
	Highlights []string
}

type CertificationVM struct {
	Period string
	Name   string
	Issuer string
	URL    string
}

type SkillsVM struct {
	Languages    []string
	Technologies []string
}

type SectionItemVM struct {
	Kind          string
	Education     *EducationVM
	Experience    *ExperienceVM
	Skills        *SkillsVM
	Project       *ProjectVM
	Certification *CertificationVM
	ShowHeader    bool
}

type ViewModel struct {
	Name     string
	Address  string
	Email    string
	Phone    string
	Website  string
	LinkedIn string
	Github   string
	Profile  string

	Sections []SectionItemVM
}

func buildViewModel(resume models.BaseResume) ViewModel {
	fullName := strings.TrimSpace(strings.Join([]string{resume.User.FirstName, resume.User.LastName}, " "))
	vm := ViewModel{
		Name:    fullName,
		Address: firstNonEmpty(resume.User.Address, resume.User.City),
		Email:   resume.User.Email,
		Phone:   resume.User.Phone,
		Website: resume.User.Website,
		Profile: stripHTMLToText(resume.User.ProfileText),
	}

	prevKind := ""
	for _, s := range resume.Sections {
		switch strings.ToLower(s.Type) {
		case "skill":
			// Per-section skills
			if m, ok := s.Data.(map[string]interface{}); ok {
				var languages []string
				var technologies []string
				if arr, ok := m["skills"].([]interface{}); ok {
					for _, it := range arr {
						if item, ok := it.(map[string]interface{}); ok {
							if skill, ok := item["skill"].(map[string]interface{}); ok {
								name := toString(skill["name"])
								category := toString(skill["category"])
								if name == "" {
									continue
								}
								if strings.EqualFold(category, "Programming Languages") {
									languages = append(languages, name)
								} else if category != "" {
									technologies = append(technologies, name)
								}
							}
						}
					}
				}
				kind := "Skills"
				vm.Sections = append(vm.Sections, SectionItemVM{Kind: kind, Skills: &SkillsVM{Languages: uniqueNonEmpty(languages), Technologies: uniqueNonEmpty(technologies)}, ShowHeader: kind != prevKind})
				prevKind = kind
			}
		case "education":
			if m, ok := s.Data.(map[string]interface{}); ok {
				inst := toString(m["institution_name"])
				degree := toString(m["degree"])
				sfy := toInt(m["started_from_year"])
				ffy := toInt(m["finished_at_year"])
				period := formatPeriod(nil, &sfy, nil, &ffy)
				highlights := htmlListToItems(toString(m["description"]))
				ed := EducationVM{Period: period, Institution: inst, Degree: degree, Highlights: highlights}
				kind := "Education"
				vm.Sections = append(vm.Sections, SectionItemVM{Kind: kind, Education: &ed, ShowHeader: kind != prevKind})
				prevKind = kind
			}
		case "experience":
			if m, ok := s.Data.(map[string]interface{}); ok {
				title := toString(m["job_title"])
				company := toString(m["company_name"])
				city := toString(m["city"])
				if ctry, ok := m["country"].(map[string]interface{}); ok {
					vmct := toString(ctry["name"])
					if vmct != "" {
						city = city + ", " + vmct
					}
				}
				sm := toIntPtr(m["started_from_month"])
				sy := toIntPtr(m["started_from_year"])
				fm := toIntPtr(m["finished_at_month"])
				fy := toIntPtr(m["finished_at_year"])
				cur := toBool(m["current"])
				period := formatPeriod(sm, sy, fm, fy)
				if cur {
					period = period + " – Present"
				}
				highlights := htmlListToItems(toString(m["description"]))
				ex := ExperienceVM{Period: period, Title: title, Company: company, City: city, Highlights: highlights}
				kind := "Experience"
				vm.Sections = append(vm.Sections, SectionItemVM{Kind: kind, Experience: &ex, ShowHeader: kind != prevKind})
				prevKind = kind
			}
		case "project":
			if m, ok := s.Data.(map[string]interface{}); ok {
				name := toString(m["name"])
				category := toString(m["category"])
				role := toString(m["role"])
				github := toString(m["github_url"])
				live := toString(m["live_url"])
				sm := toIntPtr(m["started_from_month"])
				sy := toIntPtr(m["started_from_year"])
				fm := toIntPtr(m["finished_at_month"])
				fy := toIntPtr(m["finished_at_year"])
				cur := toBool(m["current"])
				period := formatPeriod(sm, sy, fm, fy)
				if cur {
					period = period + " – Present"
				}
				highlights := htmlListToItems(toString(m["description"]))
				// skills_used as [] of maps with name
				var used []string
				if arr, ok := m["skills_used"].([]interface{}); ok {
					for _, it := range arr {
						if sk, ok := it.(map[string]interface{}); ok {
							used = append(used, toString(sk["name"]))
						}
					}
				}
				pr := ProjectVM{Period: period, Name: name, Category: category, Role: role, Github: github, Live: live, Skills: used, Highlights: highlights}
				kind := "Projects"
				vm.Sections = append(vm.Sections, SectionItemVM{Kind: kind, Project: &pr, ShowHeader: kind != prevKind})
				prevKind = kind
			}
		case "certification":
			if m, ok := s.Data.(map[string]interface{}); ok {
				name := toString(m["name"])
				issuer := toString(m["issuing_organization"])
				url := toString(m["credential_url"])
				period := toString(m["issue_date"])
				cf := CertificationVM{Period: period, Name: name, Issuer: issuer, URL: url}
				kind := "Certifications"
				vm.Sections = append(vm.Sections, SectionItemVM{Kind: kind, Certification: &cf, ShowHeader: kind != prevKind})
				prevKind = kind
			}
		}
	}

	return vm
}

func uniqueNonEmpty(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Latex escaping (minimal)
var latexReplacer = strings.NewReplacer(
	"\\", "\\\\",
	"{", "\\{",
	"}", "\\}",
	"$", "\\$",
	"&", "\\&",
	"#", "\\#",
	"_", "\\_",
	"%", "\\%",
	"~", "\\textasciitilde{}",
	"^", "\\textasciicircum{}",
)

func escapeLaTeX(s string) string { return latexReplacer.Replace(s) }

// escJoin escapes each element then joins with sep, to avoid LaTeX injection via special chars
func escJoin(slice []string, sep string) string {
	if len(slice) == 0 {
		return ""
	}
	out := make([]string, len(slice))
	for i, s := range slice {
		out[i] = escapeLaTeX(s)
	}
	return strings.Join(out, sep)
}

// Simple HTML-to-items conversion for known patterns in sample
func htmlListToItems(html string) []string {
	if strings.TrimSpace(html) == "" {
		return nil
	}
	// Extract <li>...</li>
	re := regexp.MustCompile(`(?is)<li[^>]*>\s*<p[^>]*>(.*?)</p>\s*</li>`)
	matches := re.FindAllStringSubmatch(html, -1)
	var items []string
	for _, m := range matches {
		items = append(items, stripTags(strings.TrimSpace(m[1])))
	}
	if len(items) == 0 { // fallback: strip all tags as one paragraph
		txt := stripTags(html)
		if strings.TrimSpace(txt) != "" {
			items = append(items, txt)
		}
	}
	return items
}

func stripHTMLToText(html string) string {
	items := htmlListToItems(html)
	if len(items) > 0 {
		return strings.Join(items, "; ")
	}
	return stripTags(html)
}

var tagRe = regexp.MustCompile(`(?is)<[^>]+>`)

func stripTags(s string) string { return strings.TrimSpace(tagRe.ReplaceAllString(s, "")) }

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case int32:
		return int(t)
	case int64:
		return int(t)
	default:
		return 0
	}
}

func toIntPtr(v interface{}) *int {
	if v == nil {
		return nil
	}
	i := toInt(v)
	if i == 0 {
		return nil
	}
	return &i
}

func toBool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	default:
		return false
	}
}

func formatPeriod(sm *int, sy *int, fm *int, fy *int) string {
	monthName := func(m int) string {
		if m < 1 || m > 12 {
			return ""
		}
		names := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
		return names[m-1]
	}

	var left, right string
	if sy != nil && *sy != 0 {
		if sm != nil && *sm != 0 {
			left = fmt.Sprintf("%s %d", monthName(*sm), *sy)
		} else {
			left = fmt.Sprintf("%d", *sy)
		}
	}
	if fy != nil && *fy != 0 {
		if fm != nil && *fm != 0 {
			right = fmt.Sprintf("%s %d", monthName(*fm), *fy)
		} else {
			right = fmt.Sprintf("%d", *fy)
		}
	}
	if left == "" && right == "" {
		return ""
	}
	if right == "" {
		return left + " –"
	}
	if left == "" {
		return right
	}
	return fmt.Sprintf("%s – %s", left, right)
}

// ===== DEFAULT THEME TEMPLATE =====

// Note: Closely follows sample_latex.tex, with template placeholders
const defaultThemeTemplate = `\documentclass[10pt, letterpaper]{article}

% Packages:
\usepackage[
    ignoreheadfoot,
    top=2 cm,
    bottom=2 cm,
    left=2 cm,
    right=2 cm,
    footskip=1.0 cm,
]{geometry}
\usepackage{titlesec}
\usepackage{tabularx}
\usepackage{array}
\usepackage[dvipsnames]{xcolor}
\definecolor{primaryColor}{RGB}{0, 0, 0}
\usepackage{enumitem}
\usepackage{fontawesome5}
\usepackage{amsmath}
\usepackage[
    pdftitle={{ printf "{%s's CV}" (escape .Name) }},
    pdfauthor={{ printf "{%s}" (escape .Name) }},
    pdfcreator={Letraz Utils},
    colorlinks=true,
    urlcolor=primaryColor
]{hyperref}
\usepackage[pscoord]{eso-pic}
\usepackage{calc}
\usepackage{bookmark}
\usepackage{lastpage}
\usepackage{changepage}
\usepackage{paracol}
\usepackage{ifthen}
\usepackage{needspace}
\usepackage{iftex}

\ifPDFTeX
    \pdfgentounicode=1
    \usepackage[T1]{fontenc}
    \usepackage[utf8]{inputenc}
    \usepackage{lmodern}
\fi

\usepackage{charter}

% Settings
\raggedright
\AtBeginEnvironment{adjustwidth}{\partopsep0pt}
\pagestyle{empty}
\setcounter{secnumdepth}{0}
\setlength{\parindent}{0pt}
\setlength{\topskip}{0pt}
\setlength{\columnsep}{0.15cm}
\pagenumbering{gobble}

\titleformat{\section}{\needspace{4\baselineskip}\bfseries\large}{}{0pt}{}[\vspace{1pt}\titlerule]
\titlespacing{\section}{-1pt}{0.3 cm}{0.2 cm}

\renewcommand\labelitemi{$\vcenter{\hbox{\small$\bullet$}}$}
\newenvironment{highlights}{\begin{itemize}[topsep=0.10 cm,parsep=0.10 cm,partopsep=0pt,itemsep=0pt,leftmargin=0 cm + 10pt]}{\end{itemize}}
\newenvironment{highlightsforbulletentries}{\begin{itemize}[topsep=0.10 cm,parsep=0.10 cm,partopsep=0pt,itemsep=0pt,leftmargin=10pt]}{\end{itemize}}
\newenvironment{onecolentry}{\begin{adjustwidth}{0 cm + 0.00001 cm}{0 cm + 0.00001 cm}}{\end{adjustwidth}}
\newenvironment{twocolentry}[2][]{\onecolentry\def\secondColumn{#2}\setcolumnwidth{\fill, 4.5 cm}\begin{paracol}{2}}{\switchcolumn \raggedleft \secondColumn\end{paracol}\endonecolentry}
\newenvironment{threecolentry}[3][]{\onecolentry\def\thirdColumn{#3}\setcolumnwidth{, \fill, 4.5 cm}\begin{paracol}{3}{\raggedright #2} \switchcolumn}{\switchcolumn \raggedleft \thirdColumn\end{paracol}\endonecolentry}
\newenvironment{header}{\setlength{\topsep}{0pt}\par\kern\topsep\centering\linespread{1.5}}{\par\kern\topsep}

\begin{document}
    \newcommand{\AND}{\unskip\cleaders\copy\ANDbox\hskip\wd\ANDbox\ignorespaces}
    \newsavebox\ANDbox\sbox\ANDbox{$|$}

    \begin{header}
        \fontsize{25 pt}{25 pt}\selectfont {{ escape .Name }}

        \vspace{5 pt}

        \normalsize
        {{- if .Address }}\mbox{\faGlobe}\ {{ escape .Address }}{{ end }}
        {{- if .Email }}\kern 5.0 pt\mbox{\faEnvelope}\ {\href{mailto:{{ .Email }}}{ {{ escape .Email }} }}{{ end }}
        {{- if .Phone }}\kern 5.0 pt\mbox{\faPhone}\ {\href{tel:{{ .Phone }}}{ {{ escape .Phone }} }}{{ end }}
        {{- if .LinkedIn }}\kern 5.0 pt\mbox{\faLinkedin}\ {\href{ {{ .LinkedIn }} }{LinkedIn}}{{ end }}
        {{- if .Github }}\kern 5.0 pt\mbox{\faGithub}\ {\href{ {{ .Github }} }{GitHub}}{{ end }}
        {{- if .Website }}\kern 5.0 pt\mbox{\faGlobe}\ {\href{ {{ .Website }} }{ {{ escape .Website }} }}{{ end }}
    \end{header}

    \vspace{5 pt - 0.3 cm}

    \section{Profile}
        \begin{onecolentry}
            {{ escape .Profile }}
        \end{onecolentry}

    {{- range .Sections }}
        {{- if eq .Kind "Education" }}
            {{- if .ShowHeader }}\section{Education}{{ end }}
            \begin{twocolentry}{
                {{ escape .Education.Period }}
            }
                \textbf{ {{- escape .Education.Institution -}} }, {{- escape .Education.Degree -}}\end{twocolentry}
            \vspace{0.10 cm}
            {{- if .Education.Highlights }}\begin{onecolentry}\begin{highlights}{{ range .Education.Highlights }}\item {{ escape . }}{{ end }}\end{highlights}\end{onecolentry}{{ end }}
        {{- end }}

        {{- if eq .Kind "Experience" }}
            {{- if .ShowHeader }}\section{Experience}{{ end }}
            \begin{twocolentry}{
                {{ escape .Experience.Period }}
            }
                \textbf{ {{- escape .Experience.Title -}} }, {{- escape .Experience.Company -}} -- {{- escape .Experience.City -}}\end{twocolentry}
            \vspace{0.10 cm}
            {{- if .Experience.Highlights }}\begin{onecolentry}\begin{highlights}{{ range .Experience.Highlights }}\item {{ escape . }}{{ end }}\end{highlights}\end{onecolentry}{{ end }}
            \vspace{0.2 cm}
        {{- end }}

        {{- if eq .Kind "Skills" }}
            {{- if .ShowHeader }}\section{Skills}{{ end }}
            {{- if .Skills.Languages }}\begin{onecolentry}\textbf{Languages:} {{ escJoin .Skills.Languages ", " }}\end{onecolentry}{{ end }}
            {{- if .Skills.Technologies }}\vspace{0.2 cm}\begin{onecolentry}\textbf{Technologies:} {{ escJoin .Skills.Technologies ", " }}\end{onecolentry}{{ end }}
        {{- end }}

        {{- if eq .Kind "Projects" }}
            {{- if .ShowHeader }}\section{Projects}{{ end }}
            \begin{twocolentry}{
                {{ escape .Project.Period }}
            }
                \textbf{ {{- escape .Project.Name -}} } -- \textit{ {{- escape .Project.Category -}} }\end{twocolentry}
            \vspace{0.10 cm}
            \begin{onecolentry}
                {{- if .Project.Role }}{{ escape .Project.Role }}{{ end }}
                {{- if or .Project.Github .Project.Live }} $|$ {{ end }}
                {{- if .Project.Github }}\href{ {{ .Project.Github }} }{\faGithub\ GitHub}{{ end }}
                {{- if and .Project.Github .Project.Live }} $|$ {{ end }}
                {{- if .Project.Live }}\href{ {{ .Project.Live }} }{Live Demo}{{ end }}
            \end{onecolentry}
            {{- if .Project.Skills }}\vspace{0.10 cm}\begin{onecolentry}\textbf{Skills used:} {{ escJoin .Project.Skills ", " }}\end{onecolentry}{{ end }}
            {{- if .Project.Highlights }}\vspace{0.10 cm}\begin{onecolentry}\begin{highlights}{{ range .Project.Highlights }}\item {{ escape . }}{{ end }}\end{highlights}\end{onecolentry}{{ end }}
            \vspace{0.2 cm}
        {{- end }}

        {{- if eq .Kind "Certifications" }}
            {{- if .ShowHeader }}\section{Certifications}{{ end }}
            \begin{twocolentry}{
                {{ escape .Certification.Period }}
            }
                \textbf{ {{- escape .Certification.Name -}} }\end{twocolentry}
            {{- if or .Certification.Issuer .Certification.URL }}\vspace{0.10 cm}\begin{onecolentry}{{ if .Certification.Issuer }}{{ escape .Certification.Issuer }}{{ end }} {{ if and .Certification.Issuer .Certification.URL }} $|$ {{ end }} {{ if .Certification.URL }}\href{ {{ .Certification.URL }} }{View Certificate}{{ end }}\end{onecolentry}{{ end }}
            \vspace{0.2 cm}
        {{- end }}
    {{- end }}

\end{document}
`
