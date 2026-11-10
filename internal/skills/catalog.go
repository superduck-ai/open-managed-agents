package skills

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

type skillMetadata struct {
	Name        string
	Description string
	License     string
}

func parseSkillMetadata(data []byte, fallbackName string) skillMetadata {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	meta := parseFrontmatter(text)
	name := firstNonEmpty(meta["name"], firstMarkdownHeading(text), fallbackName)
	description := firstNonEmpty(meta["description"], firstMarkdownParagraph(text))
	return skillMetadata{
		Name:        name,
		Description: description,
		License:     meta["license"],
	}
}

func parseFrontmatter(text string) map[string]string {
	values := map[string]string{}
	if !strings.HasPrefix(text, "---\n") {
		return values
	}
	end := strings.Index(text[len("---\n"):], "\n---")
	if end < 0 {
		return values
	}
	block := text[len("---\n") : len("---\n")+end]
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(block), &fields); err != nil {
		return values
	}
	for _, key := range []string{"name", "description", "license"} {
		if field, ok := fields[key]; ok && field.Tag == "!!str" {
			values[key] = field.Value
		}
	}

	return values
}

func firstMarkdownHeading(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return ""
}

func firstMarkdownParagraph(text string) string {
	if strings.HasPrefix(text, "---\n") {
		if end := strings.Index(text[len("---\n"):], "\n---"); end >= 0 {
			text = text[len("---\n")+end+len("\n---"):]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
