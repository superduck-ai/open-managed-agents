package skills

import (
	"strconv"
	"strings"
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
	for _, line := range strings.Split(block, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		} else {
			value = strings.Trim(value, `"'`)
		}
		values[key] = value
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
