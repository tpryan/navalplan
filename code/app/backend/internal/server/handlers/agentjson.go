package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	bulletKeyRegex = regexp.MustCompile(`(?m)^(\s*)[-*]\s*("[\w_-]+"\s*:)`)
	jsonMathRegex  = regexp.MustCompile(`(-?\d+\.?\d*)\s*([+-])\s*(\d+\.?\d*)`)
)

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")

	start := -1
	if startObj != -1 && (startArr == -1 || startObj < startArr) {
		start = startObj
	} else {
		start = startArr
	}

	endObj := strings.LastIndex(s, "}")
	endArr := strings.LastIndex(s, "]")

	end := -1
	if endObj != -1 && (endArr == -1 || endObj > endArr) {
		end = endObj
	} else {
		end = endArr
	}

	if start != -1 && end != -1 && end > start {
		s = s[start : end+1]
	} else {
		return ""
	}

	s = bulletKeyRegex.ReplaceAllString(s, "$1$2")
	s = strings.ReplaceAll(s, `\'`, `'`)
	return strings.TrimSpace(s)
}

func repairTruncatedJSONArray(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if json.Valid([]byte(s)) {
		return s
	}

	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")

	if startObj == -1 && startArr == -1 {
		return s
	}

	isObjRoot := startObj != -1 && (startArr == -1 || startObj < startArr)
	start := startObj
	if !isObjRoot {
		start = startArr
	}

	// Scan backwards from the end for each '}' to find the last valid closure
	for i := len(s) - 1; i >= start; i-- {
		if s[i] == '}' {
			sub := s[start : i+1]
			if isObjRoot {
				// Try closing array and object wrapper: ...]}
				candidate := sub + "\n]}"
				if json.Valid([]byte(candidate)) {
					return candidate
				}
				// Try closing object only: ...}
				candidateObj := sub + "\n}"
				if json.Valid([]byte(candidateObj)) {
					return candidateObj
				}
			} else {
				// Array root: ...]
				candidate := sub + "\n]"
				if json.Valid([]byte(candidate)) {
					return candidate
				}
			}
		}
	}

	return s
}

// repairMathInJSON evaluates inline arithmetic expressions (e.g. `41.5 + 0.02`)
// emitted by the LLM in unquoted JSON value positions while leaving quoted JSON
// string literals untouched so date ranges (`2026-04`), VHF channels (`16-68`),
// and hyphenated prose are never mangled.
func repairMathInJSON(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))

	inString := false
	escaped := false
	segStart := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				sb.WriteString(s[segStart : i+1])
				inString = false
				segStart = i + 1
			}
		} else if ch == '"' {
			sb.WriteString(evalUnquotedMath(s[segStart:i]))
			inString = true
			segStart = i
		}
	}

	if segStart < len(s) {
		if inString {
			sb.WriteString(s[segStart:])
		} else {
			sb.WriteString(evalUnquotedMath(s[segStart:]))
		}
	}

	return sb.String()
}

func evalUnquotedMath(segment string) string {
	return jsonMathRegex.ReplaceAllStringFunc(segment, func(match string) string {
		parts := jsonMathRegex.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		v1, err1 := strconv.ParseFloat(parts[1], 64)
		v2, err2 := strconv.ParseFloat(parts[3], 64)
		if err1 != nil || err2 != nil {
			return match
		}
		op := parts[2]
		res := v1 + v2
		if op == "-" {
			res = v1 - v2
		}
		return fmt.Sprintf("%.7f", res)
	})
}
