package latex

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"letraz-utils/pkg/models"
)

//go:embed themes/*.tex
var themesFS embed.FS

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
		// Load default theme from embedded filesystem
		content, err := themesFS.ReadFile("themes/default.tex")
		if err != nil {
			return "", fmt.Errorf("failed to load default theme: %w", err)
		}
		return string(content), nil
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
	Categories    map[string][]string // Map of category name to skills
	Uncategorized []string            // Skills without category
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
				categories := make(map[string][]string)
				var uncategorized []string
				if arr, ok := m["skills"].([]interface{}); ok {
					for _, it := range arr {
						if item, ok := it.(map[string]interface{}); ok {
							if skill, ok := item["skill"].(map[string]interface{}); ok {
								name := toString(skill["name"])
								category := toString(skill["category"])
								if name == "" {
									continue
								}
								// Group skills by category
								if category == "" || category == "null" {
									uncategorized = append(uncategorized, name)
								} else {
									if categories[category] == nil {
										categories[category] = []string{}
									}
									categories[category] = append(categories[category], name)
								}
							}
						}
					}
				}
				// Remove duplicates from each category
				for cat, skills := range categories {
					categories[cat] = uniqueNonEmpty(skills)
				}
				kind := "Skills"
				vm.Sections = append(vm.Sections, SectionItemVM{Kind: kind, Skills: &SkillsVM{Categories: categories, Uncategorized: uniqueNonEmpty(uncategorized)}, ShowHeader: kind != prevKind})
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
				country := ""
				if ctry, ok := m["country"].(map[string]interface{}); ok {
					country = toString(ctry["name"])
				}
				// Build location string properly
				location := ""
				if city != "" && country != "" {
					location = city + ", " + country
				} else if city != "" {
					location = city
				} else if country != "" {
					location = country
				}
				sm := toIntPtr(m["started_from_month"])
				sy := toIntPtr(m["started_from_year"])
				fm := toIntPtr(m["finished_at_month"])
				fy := toIntPtr(m["finished_at_year"])
				cur := toBool(m["current"])
				var period string
				if cur {
					base := formatPeriod(sm, sy, nil, nil)
					base = strings.TrimSuffix(base, " –")
					period = base + " – Present"
				} else {
					period = formatPeriod(sm, sy, fm, fy)
				}
				highlights := htmlListToItems(toString(m["description"]))
				ex := ExperienceVM{Period: period, Title: title, Company: company, City: location, Highlights: highlights}
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
				var period string
				if cur {
					base := formatPeriod(sm, sy, nil, nil)
					base = strings.TrimSuffix(base, " –")
					period = base + " – Present"
				} else {
					period = formatPeriod(sm, sy, fm, fy)
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
