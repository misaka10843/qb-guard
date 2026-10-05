
package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type matchResult int

const (
	matchDefault matchResult = iota
	matchTrue
	matchFalse
)

type Rule struct {
	Method  string          `json:"method"`
	Content string          `json:"content"`
	Min     int             `json:"min"`
	Max     int             `json:"max"`
	If      json.RawMessage `json:"if"`
	Hit     string          `json:"hit"`
	Miss    string          `json:"miss"`

	content string
	re      *regexp.Regexp
	cond    *Rule
	hitRes  matchResult
	missRes matchResult
}

func parseRules(raw []string) ([]Rule, error) {
	rules := make([]Rule, 0, len(raw))
	for i, line := range raw {
		var r Rule
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("第 %d 条规则 JSON 解析失败: %w", i+1, err)
		}
		if err := r.compile(); err != nil {
			return nil, fmt.Errorf("第 %d 条规则编译失败: %w", i+1, err)
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func (r *Rule) compile() error {
	r.hitRes = matchTrue
	r.missRes = matchDefault
	if r.Hit != "" {
		res, err := parseMatchResult(r.Hit)
		if err != nil {
			return err
		}
		r.hitRes = res
	}
	if r.Miss != "" {
		res, err := parseMatchResult(r.Miss)
		if err != nil {
			return err
		}
		r.missRes = res
	}
	switch r.Method {
	case "STARTS_WITH", "ENDS_WITH", "CONTAINS", "EQUALS":
		r.content = strings.ToLower(r.Content)
	case "LENGTH":
	case "REGEX":
		re, err := regexp.Compile("^(?:" + r.Content + ")$")
		if err != nil {
			return err
		}
		r.re = re
	default:
		return fmt.Errorf("不支持的 method: %q", r.Method)
	}
	if len(r.If) > 0 {
		cond, err := parseRuleIf(r.If)
		if err != nil {
			return fmt.Errorf("if 条件解析失败: %w", err)
		}
		r.cond = cond
	}
	return nil
}

func parseMatchResult(s string) (matchResult, error) {
	switch strings.ToUpper(s) {
	case "TRUE":
		return matchTrue, nil
	case "FALSE":
		return matchFalse, nil
	case "DEFAULT":
		return matchDefault, nil
	}
	return matchDefault, fmt.Errorf("不支持的 hit/miss 值: %q", s)
}

func parseRuleIf(raw json.RawMessage) (*Rule, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil && obj != nil {
		var r Rule
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		return &r, r.compile()
	}
	var literal any
	if err := json.Unmarshal(raw, &literal); err != nil {
		return nil, err
	}
	truthy := false
	switch v := literal.(type) {
	case nil:
		truthy = true
	case bool:
		truthy = v
	case float64:
		truthy = v != 0
	case string:
		truthy = strings.EqualFold(v, "true")
	default:
		return nil, fmt.Errorf("if 只接受对象/布尔/数字")
	}
	if truthy {
		return &Rule{Method: "CONST", hitRes: matchTrue, missRes: matchTrue}, nil
	}
	return &Rule{Method: "CONST", hitRes: matchFalse, missRes: matchFalse}, nil
}

func (r *Rule) match(content string) matchResult {
	if r.cond != nil && r.cond.match(content) == matchFalse {
		return matchFalse
	}
	if r.Method == "CONST" {
		return r.hitRes
	}
	hit := false
	switch r.Method {
	case "STARTS_WITH":
		hit = strings.HasPrefix(strings.ToLower(content), r.content)
	case "ENDS_WITH":
		hit = strings.HasSuffix(strings.ToLower(content), r.content)
	case "CONTAINS":
		hit = strings.Contains(strings.ToLower(content), r.content)
	case "EQUALS":
		hit = strings.EqualFold(content, r.content)
	case "LENGTH":
		n := len([]rune(content))
		hit = n >= r.Min && n <= r.Max
	case "REGEX":
		hit = r.re.MatchString(content)
	}
	if hit {
		return r.hitRes
	}
	return r.missRes
}

func (r *Rule) describe() string {
	switch r.Method {
	case "LENGTH":
		return fmt.Sprintf("LENGTH min=%d max=%d", r.Min, r.Max)
	case "CONST":
		if r.hitRes == matchTrue {
			return "if(true)"
		}
		return "if(false)"
	default:
		return fmt.Sprintf("%s %q", r.Method, r.Content)
	}
}

func matchRules(rules []Rule, content string) (bool, *Rule) {
	var matched *Rule
	for i := range rules {
		switch rules[i].match(content) {
		case matchFalse:
			return false, &rules[i]
		case matchTrue:
			matched = &rules[i]
		}
	}
	return matched != nil, matched
}
