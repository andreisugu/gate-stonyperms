package command

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	c "go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/component/codec/legacy"
)

var legacyCodec = &legacy.Legacy{Char: legacy.AmpersandChar}

// Msg formats a legacy ampersand-colored string with optional format arguments.
func Msg(format string, a ...interface{}) c.Component {
	text := format
	if len(a) > 0 {
		text = fmt.Sprintf(format, a...)
	}
	comp, err := legacyCodec.Unmarshal([]byte(text))
	if err != nil {
		return &c.Text{Content: text}
	}
	return comp
}

// MsgPlain converts a pre-formatted string directly to a Component without sprintf processing.
func MsgPlain(text string) c.Component {
	comp, err := legacyCodec.Unmarshal([]byte(text))
	if err != nil {
		return &c.Text{Content: text}
	}
	return comp
}

// Prefix returns the standard StonyPerms [SP] message prefix.
const Prefix = "&6[SP]&r "

// Header formats a LuckPerms-style section header.
func Header(title string) string {
	line := strings.Repeat("=", 16)
	return fmt.Sprintf("&b%s &e&l%s &b%s&r", line, title, line)
}

// SubHeader formats a subsection line.
func SubHeader(title string) string {
	return fmt.Sprintf("&3> &b&l%s:&r", title)
}

// Separator returns a horizontal divider bar.
func Separator() string {
	return "&8" + strings.Repeat("-", 42) + "&r"
}

// FormatBool returns colored true/false string.
func FormatBool(b bool) string {
	if b {
		return "&atrue&r"
	}
	return "&cfalse&r"
}

// FormatDuration formats a time.Duration into a clean human-readable string like "2d 4h 15m".
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
}

// ParseDuration parses human duration strings like "10s", "5m", "2h", "7d", "30d", "1y".
func ParseDuration(input string) (time.Duration, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return 0, fmt.Errorf("empty duration")
	}

	if strings.HasSuffix(input, "d") {
		daysStr := strings.TrimSuffix(input, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration: %s", input)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if strings.HasSuffix(input, "w") {
		weeksStr := strings.TrimSuffix(input, "w")
		weeks, err := strconv.Atoi(weeksStr)
		if err != nil {
			return 0, fmt.Errorf("invalid week duration: %s", input)
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(input, "mo") {
		moStr := strings.TrimSuffix(input, "mo")
		mo, err := strconv.Atoi(moStr)
		if err != nil {
			return 0, fmt.Errorf("invalid month duration: %s", input)
		}
		return time.Duration(mo) * 30 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(input, "y") {
		yStr := strings.TrimSuffix(input, "y")
		y, err := strconv.Atoi(yStr)
		if err != nil {
			return 0, fmt.Errorf("invalid year duration: %s", input)
		}
		return time.Duration(y) * 365 * 24 * time.Hour, nil
	}

	return time.ParseDuration(input)
}

// ParseContexts extracts key=value context pairs from command arguments.
func ParseContexts(args []string) (map[string]string, []string) {
	contexts := make(map[string]string)
	var remaining []string

	for _, arg := range args {
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			k := strings.TrimSpace(strings.ToLower(parts[0]))
			v := strings.TrimSpace(parts[1])
			if k != "" && v != "" {
				contexts[k] = v
				continue
			}
		}
		remaining = append(remaining, arg)
	}

	return contexts, remaining
}

// FormatContexts formats a context map into "[server=lobby, world=hub]".
func FormatContexts(contexts map[string]string) string {
	if len(contexts) == 0 {
		return ""
	}
	var pairs []string
	for k, v := range contexts {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return "&8[" + strings.Join(pairs, ", ") + "]&r"
}
