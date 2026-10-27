package content

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	mediaFeature = regexp.MustCompile(`^\(\s*([a-z-]+)\s*:\s*([a-z0-9.]+)\s*\)$`)
	mediaAnd     = regexp.MustCompile(`\s+and\s+`)
	cssLength    = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(px|em|rem|vw|vh|vmin|vmax)?$`)
	resolution   = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(dpi|dppx|dpcm)$`)
	sourceSize   = regexp.MustCompile(`(?:calc\([^()]+\)|[0-9]+(?:\.[0-9]+)?(?:px|em|rem|vw|vh|vmin|vmax)?)$`)
)

// mediaCondition accepts a bounded, positive query list. Unsupported conditions
// reject the whole list, so conditional content never becomes unconditional.
func mediaCondition(source string, allowType bool) (string, bool) {
	if len(source) > 1024 {
		return "", false
	}
	source = strings.ToLower(strings.Join(cssFields(source), " "))
	if source == "" {
		return "", true
	}
	queries := strings.Split(source, ",")
	if len(queries) > 8 {
		return "", false
	}
	for i, query := range queries {
		parts := mediaAnd.Split(cssTrim(query), -1)
		if len(parts) > 8 {
			return "", false
		}
		for j, part := range parts {
			part = cssTrim(part)
			if j == 0 && allowType && (part == "all" || part == "screen" || part == "print" || part == "only screen" || part == "only print" || part == "only all") {
				parts[j] = part
				continue
			}
			match := mediaFeature.FindStringSubmatch(part)
			if match == nil {
				return "", false
			}
			name, value := match[1], match[2]
			feature := strings.TrimPrefix(name, "min-")
			if feature == name {
				feature = strings.TrimPrefix(name, "max-")
			}
			valid := false
			switch feature {
			case "width", "height":
				valid = validLength(value, false)
			case "resolution":
				m := resolution.FindStringSubmatch(value)
				valid = m != nil && boundedNumber(m[1], false)
			case "orientation":
				valid = name == feature && (value == "portrait" || value == "landscape")
			case "prefers-color-scheme":
				valid = name == feature && (value == "light" || value == "dark")
			}
			if !valid {
				return "", false
			}
			parts[j] = "(" + name + ":" + value + ")"
		}
		queries[i] = strings.Join(parts, " and ")
	}
	return strings.Join(queries, ", "), true
}

func boundedNumber(value string, zero bool) bool {
	n, err := strconv.ParseFloat(value, 64)
	return err == nil && n <= 100000 && (n > 0 || zero && n == 0)
}

func validLength(value string, viewport bool) bool {
	m := cssLength.FindStringSubmatch(value)
	if m == nil || !boundedNumber(m[1], true) {
		return false
	}
	if m[2] == "" {
		return value == "0"
	}
	return viewport || m[2] == "px" || m[2] == "em" || m[2] == "rem"
}

func validSourceSize(value string) bool {
	if validLength(value, true) {
		return true
	}
	if !strings.HasPrefix(value, "calc(") || !strings.HasSuffix(value, ")") {
		return false
	}
	terms := cssFields(value[5 : len(value)-1])
	if len(terms) < 3 || len(terms) > 15 || len(terms)%2 == 0 {
		return false
	}
	for i, term := range terms {
		if i%2 == 0 {
			if !validLength(term, true) {
				return false
			}
		} else if term != "+" && term != "-" {
			return false
		}
	}
	return true
}

// sizes supports lengths and flat calc sums/differences, with the same media
// feature subset as stylesheets. A rejected list invalidates its source set.
func imageSizes(source string) (string, bool) {
	if len(source) > 2048 {
		return "", false
	}
	source = strings.ToLower(strings.Join(cssFields(source), " "))
	if source == "" {
		return "", true
	}
	// Neither supported size expressions nor media features contain commas.
	entries := strings.Split(source, ",")
	if len(entries) > 16 {
		return "", false
	}
	for i, entry := range entries {
		entry = cssTrim(entry)
		loc := sourceSize.FindStringIndex(entry)
		if loc == nil || loc[0] > 0 && entry[loc[0]-1] != ' ' {
			return "", false
		}
		size := entry[loc[0]:]
		if !validSourceSize(size) {
			return "", false
		}
		condition, ok := mediaCondition(cssTrim(entry[:loc[0]]), false)
		if !ok || strings.Contains(condition, ",") {
			return "", false
		}
		if condition == "" {
			if i != len(entries)-1 {
				return "", false
			}
			entries[i] = size
		} else {
			entries[i] = condition + " " + size
		}
	}
	return strings.Join(entries, ", "), true
}

func cssFields(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune(" \t\r\n\f", r) })
}

func cssTrim(value string) string { return strings.Trim(value, " \t\r\n\f") }
