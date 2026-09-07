package skills

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const (
	KindLibrary  = "library"
	KindPersonal = "personal"
	DefaultName  = "product-self-knowledge"
)

type Meta struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license"`
	Compatibility string            `yaml:"compatibility"`
	Metadata      map[string]string `yaml:"metadata"`
	Body          string
	Raw           string
}

var nameRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func Parse(raw string) (Meta, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(strings.TrimSpace(raw), "---") {
		return Meta{}, fmt.Errorf("SKILL.md needs YAML frontmatter")
	}
	rest := strings.TrimSpace(raw)
	rest = strings.TrimPrefix(rest, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Meta{}, fmt.Errorf("SKILL.md frontmatter is not closed")
	}
	fm := rest[:end]
	body := strings.TrimPrefix(rest[end+len("\n---"):], "\n")
	var m Meta
	if err := yaml.Unmarshal([]byte(fm), &m); err != nil {
		return Meta{}, fmt.Errorf("SKILL.md frontmatter: %w", err)
	}
	m.Name = strings.TrimSpace(m.Name)
	m.Description = strings.TrimSpace(m.Description)
	m.Body = body
	m.Raw = raw
	if err := Validate(m); err != nil {
		return Meta{}, err
	}
	return m, nil
}

func Validate(m Meta) error {
	if err := ValidName(m.Name); err != nil {
		return err
	}
	n := utf8.RuneCountInString(m.Description)
	if n < 1 || n > 1024 {
		return fmt.Errorf("description must be 1–1024 characters")
	}
	if c := strings.TrimSpace(m.Compatibility); c != "" && utf8.RuneCountInString(c) > 500 {
		return fmt.Errorf("compatibility must be at most 500 characters")
	}
	return nil
}

func ValidName(name string) error {
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		return fmt.Errorf("skill name must be 1–64 characters")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("skill name %q must be lowercase letters, numbers, and single hyphens", name)
	}
	return nil
}

func MatchDir(name, dir string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if name != dir {
		return fmt.Errorf("skill name %q must match directory %q", name, dir)
	}
	return nil
}
