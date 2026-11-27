// Package serverprops parses and validates Minecraft Java server.properties.
// The schema is version-gated: removed keys are recognized but warned about,
// and keys introduced in a newer version are called out.
package serverprops

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Key describes one server.properties setting.
type Key struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
	Allowed     string   `json:"allowed,omitempty"`
	Since       string   `json:"since,omitempty"`
	Until       string   `json:"until,omitempty"`
	Values      []string `json:"-"`
	Min         *int64   `json:"-"`
	Max         *int64   `json:"-"`
}

// Result is the outcome of validating a file.
type Result struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

var (
	keys   = buildKeys()
	byName = func() map[string]Key {
		m := make(map[string]Key, len(keys))
		for _, k := range keys {
			m[k.Name] = k
		}
		return m
	}()
	sha1Pattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	secret40    = regexp.MustCompile(`^[A-Za-z0-9]{40}$`)
)

// Reference returns the keys available in a Minecraft version, sorted by name.
// An empty version returns every known current key, including removed keys so
// the caller can show a useful reference when the server version is unknown.
func Reference(version string) []Key {
	out := make([]Key, 0, len(keys))
	for _, k := range keys {
		if version == "" || available(k, version) {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Parse returns the key/value entries in a properties file with their line
// numbers. Comments, blank lines, escapes, and continuations are handled.
type Entry struct {
	Key   string
	Value string
	Line  int
}

func Parse(content string) []Entry {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var out []Entry
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		lineNo := i + 1
		for endsWithContinuation(line) && i+1 < len(lines) {
			line = line[:len(line)-1] + strings.TrimLeft(lines[i+1], " \t\f")
			i++
		}
		trimmed := strings.TrimLeft(line, " \t\f")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		key, value := splitEntry(trimmed)
		key = unescape(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		out = append(out, Entry{Key: key, Value: unescape(strings.TrimSpace(value)), Line: lineNo})
	}
	return out
}

func splitEntry(line string) (string, string) {
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '=' || line[i] == ':' {
			return line[:i], line[i+1:]
		}
		if line[i] == ' ' || line[i] == '\t' || line[i] == '\f' {
			return line[:i], line[i+1:]
		}
	}
	return line, ""
}

func endsWithContinuation(line string) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				if n, err := strconv.ParseUint(s[i+1:i+5], 16, 16); err == nil {
					b.WriteRune(rune(n))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// Validate parses content and checks every known key and value.
func Validate(content, version string) Result {
	var res Result
	entries := Parse(content)
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		key, ok := byName[entry.Key]
		if !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("line %d: unknown property %q", entry.Line, entry.Key))
			continue
		}
		if first, dup := seen[entry.Key]; dup {
			res.Warnings = append(res.Warnings, fmt.Sprintf("line %d: duplicate property %q (first on line %d)", entry.Line, entry.Key, first))
		} else {
			seen[entry.Key] = entry.Line
		}
		if version != "" {
			if key.Since != "" && versionLess(version, key.Since) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("line %d: %s was added in %s", entry.Line, key.Name, key.Since))
			}
			if key.Until != "" && !versionLess(version, key.Until) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("line %d: %s was removed in %s", entry.Line, key.Name, key.Until))
			}
		}
		if err := validateValue(key, entry.Value); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: %s: %v", entry.Line, key.Name, err))
		}
	}
	if err := validateCross(entries); err != "" {
		res.Errors = append(res.Errors, err)
	}
	res.Valid = len(res.Errors) == 0
	return res
}

func validateValue(k Key, value string) error {
	switch k.Type {
	case "boolean":
		if value != "true" && value != "false" {
			return fmt.Errorf("must be true or false")
		}
	case "integer":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("must be an integer")
		}
		if k.Min != nil && n < *k.Min {
			return fmt.Errorf("must be at least %d", *k.Min)
		}
		if k.Max != nil && n > *k.Max {
			return fmt.Errorf("must be at most %d", *k.Max)
		}
	case "enum":
		if !contains(k.Values, value) {
			return fmt.Errorf("must be one of %s", k.Allowed)
		}
	case "level-type":
		v := strings.ToLower(value)
		if !contains(k.Values, v) && !strings.Contains(v, ":") {
			return fmt.Errorf("must be a known level type or a namespaced datapack preset")
		}
	case "port":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 || n > 65534 {
			return fmt.Errorf("must be a port between 1 and 65534")
		}
	case "ip":
		if value != "" && net.ParseIP(value) == nil {
			return fmt.Errorf("must be an IP address or empty")
		}
	case "uuid":
		if value != "" && !uuidPattern.MatchString(value) {
			return fmt.Errorf("must be a hyphenated UUID or empty")
		}
	case "sha1":
		if value != "" && !sha1Pattern.MatchString(value) {
			return fmt.Errorf("must be 40 hexadecimal characters or empty")
		}
	case "secret40":
		if value != "" && !secret40.MatchString(value) {
			return fmt.Errorf("must be 40 letters/digits or empty")
		}
	}
	return nil
}

func validateCross(entries []Entry) string {
	values := make(map[string]Entry, len(entries))
	for _, e := range entries {
		values[e.Key] = e
	}
	if e, ok := values["enable-rcon"]; ok && e.Value == "true" {
		if r, ok := values["rcon.password"]; !ok || r.Value == "" {
			return fmt.Sprintf("line %d: rcon.password is required when enable-rcon=true", e.Line)
		}
	}
	if e, ok := values["management-server-enabled"]; ok && e.Value == "true" {
		tls := true
		if t, ok := values["management-server-tls-enabled"]; ok {
			tls = t.Value != "false"
		}
		if tls {
			if ks, ok := values["management-server-tls-keystore"]; !ok || ks.Value == "" {
				return fmt.Sprintf("line %d: management-server-tls-keystore is required when TLS is enabled", e.Line)
			}
		}
	}
	return ""
}

func available(k Key, version string) bool {
	if k.Since != "" && versionLess(version, k.Since) {
		return false
	}
	if k.Until != "" && !versionLess(version, k.Until) {
		return false
	}
	return true
}

func versionLess(a, b string) bool {
	as, bs := versionParts(a), versionParts(b)
	if len(as) == 0 || len(bs) == 0 {
		return false
	}
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}

func versionParts(v string) []int {
	parts := strings.Split(strings.TrimSpace(v), ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if strings.EqualFold(v, value) {
			return true
		}
	}
	return false
}

func buildKeys() []Key {
	return []Key{
		{Name: "accepts-transfers", Type: "boolean", Default: "false", Description: "Accept players transferred from another server.", Since: "1.20.5"},
		{Name: "allow-flight", Type: "boolean", Default: "false", Description: "Allow Survival players to fly without being kicked."},
		{Name: "allow-nether", Type: "boolean", Default: "true", Description: "Allow travel to the Nether. Now a game rule.", Until: "1.21.9"},
		{Name: "broadcast-console-to-ops", Type: "boolean", Default: "true", Description: "Show console command output to online operators."},
		{Name: "broadcast-rcon-to-ops", Type: "boolean", Default: "true", Description: "Show RCON command output to online operators."},
		{Name: "bug-report-link", Type: "string", Default: "", Description: "URL shown to players for reporting bugs.", Since: "1.21"},
		{Name: "debug", Type: "boolean", Default: "false", Description: "Enable extra server debug logging."},
		{Name: "difficulty", Type: "enum", Default: "easy", Description: "World difficulty.", Allowed: "peaceful, easy, normal, hard (or 0-3)", Values: []string{"peaceful", "easy", "normal", "hard", "0", "1", "2", "3"}},
		{Name: "enable-code-of-conduct", Type: "boolean", Default: "false", Description: "Show a code of conduct from the codeofconduct folder on join.", Since: "1.21.9"},
		{Name: "enable-command-block", Type: "boolean", Default: "false", Description: "Allow command blocks. Now a game rule.", Until: "1.21.9"},
		{Name: "enable-jmx-monitoring", Type: "boolean", Default: "false", Description: "Expose tick times through JMX."},
		{Name: "enable-query", Type: "boolean", Default: "false", Description: "Enable the GameSpy4 query listener."},
		{Name: "enable-rcon", Type: "boolean", Default: "false", Description: "Enable the unencrypted RCON remote console."},
		{Name: "enable-status", Type: "boolean", Default: "true", Description: "Answer server-list status requests."},
		{Name: "enforce-secure-profile", Type: "boolean", Default: "true", Description: "Require a Mojang-signed chat profile to join.", Since: "1.19"},
		{Name: "enforce-whitelist", Type: "boolean", Default: "false", Description: "Kick online players who are not whitelisted when the list changes."},
		{Name: "entity-broadcast-range-percentage", Type: "integer", Default: "100", Description: "Percentage of normal entity broadcast range.", Min: new(int64(10)), Max: new(int64(1000)), Allowed: "10-1000"},
		{Name: "force-gamemode", Type: "boolean", Default: "false", Description: "Put joining players in the default game mode."},
		{Name: "function-permission-level", Type: "integer", Default: "2", Description: "Default permission level for functions.", Min: new(int64(1)), Max: new(int64(4)), Allowed: "1-4"},
		{Name: "gamemode", Type: "enum", Default: "survival", Description: "Default game mode for new players.", Allowed: "survival, creative, adventure, spectator (or 0-3)", Values: []string{"survival", "creative", "adventure", "spectator", "0", "1", "2", "3"}},
		{Name: "generate-structures", Type: "boolean", Default: "true", Description: "Generate structures in new chunks."},
		{Name: "generator-settings", Type: "string", Default: "{}", Description: "World-generator settings; used by flat and custom presets."},
		{Name: "hardcore", Type: "boolean", Default: "false", Description: "Enable hardcore mode for the created world."},
		{Name: "hide-online-players", Type: "boolean", Default: "false", Description: "Omit the player list from server-list responses.", Since: "1.18"},
		{Name: "initial-disabled-packs", Type: "string", Default: "", Description: "Comma-separated data packs not auto-enabled for a new world.", Since: "1.19.3"},
		{Name: "initial-enabled-packs", Type: "string", Default: "vanilla", Description: "Comma-separated data packs enabled for a new world.", Since: "1.19.3"},
		{Name: "level-name", Type: "string", Default: "world", Description: "World directory to load or create."},
		{Name: "level-seed", Type: "string", Default: "", Description: "Seed for a new world; empty picks a random seed."},
		{Name: "level-type", Type: "level-type", Default: "minecraft:normal", Description: "World preset for a new world.", Allowed: "minecraft:normal, flat, large_biomes, amplified, single_biome_surface, or a namespaced preset", Values: []string{"minecraft:normal", "minecraft:flat", "minecraft:large_biomes", "minecraft:amplified", "minecraft:single_biome_surface", "normal", "flat", "large_biomes", "amplified", "single_biome_surface", "default"}},
		{Name: "log-ips", Type: "boolean", Default: "true", Description: "Write player IP addresses to the log.", Since: "1.20.2"},
		{Name: "management-server-allowed-origins", Type: "string", Default: "", Description: "Comma-separated browser origins allowed to sign in.", Since: "1.21.11"},
		{Name: "management-server-enabled", Type: "boolean", Default: "false", Description: "Enable the Minecraft Server Management Protocol.", Since: "1.21.9"},
		{Name: "management-server-host", Type: "string", Default: "localhost", Description: "Host for the management protocol listener.", Since: "1.21.9"},
		{Name: "management-server-port", Type: "integer", Default: "0", Description: "Management protocol port; 0 picks a free port.", Min: new(int64(0)), Max: new(int64(65535)), Allowed: "0-65535", Since: "1.21.9"},
		{Name: "management-server-secret", Type: "secret40", Default: "", Description: "40-character management authentication secret; empty is generated.", Since: "1.21.9"},
		{Name: "management-server-tls-enabled", Type: "boolean", Default: "true", Description: "Use TLS for the management protocol.", Since: "1.21.9"},
		{Name: "management-server-tls-keystore", Type: "string", Default: "", Description: "Path to the management TLS keystore.", Since: "1.21.9"},
		{Name: "management-server-tls-keystore-password", Type: "string", Default: "", Description: "Password for the management TLS keystore.", Since: "1.21.9"},
		{Name: "max-chained-neighbor-updates", Type: "integer", Default: "1000000", Description: "Limit consecutive neighbor updates; negative removes the limit.", Since: "1.19"},
		{Name: "max-players", Type: "integer", Default: "20", Description: "Maximum simultaneous players.", Min: new(int64(0)), Max: new(int64(2147483647)), Allowed: "0-2147483647"},
		{Name: "max-tick-time", Type: "integer", Default: "60000", Description: "Watchdog threshold in milliseconds; -1 or 0 disables it.", Min: new(int64(-1)), Allowed: "-1, or 0 and above"},
		{Name: "max-world-size", Type: "integer", Default: "29999984", Description: "Maximum world-border radius in blocks.", Min: new(int64(1)), Max: new(int64(29999984)), Allowed: "1-29999984"},
		{Name: "motd", Type: "string", Default: "A Minecraft Server", Description: "Text shown below the server name in the multiplayer list."},
		{Name: "network-compression-threshold", Type: "integer", Default: "256", Description: "Minimum packet size to compress; -1 disables compression.", Min: new(int64(-1)), Allowed: "-1, or a byte count"},
		{Name: "online-mode", Type: "boolean", Default: "true", Description: "Verify players with the Minecraft account service."},
		{Name: "op-permission-level", Type: "integer", Default: "4", Description: "Default permission level granted by /op.", Min: new(int64(0)), Max: new(int64(4)), Allowed: "0-4"},
		{Name: "pause-when-empty-seconds", Type: "integer", Default: "60", Description: "Pause after this many seconds with no players; negative disables.", Since: "1.21.2"},
		{Name: "player-idle-timeout", Type: "integer", Default: "0", Description: "Kick idle players after this many minutes; 0 disables.", Min: new(int64(0)), Allowed: "0 or more"},
		{Name: "prevent-proxy-connections", Type: "boolean", Default: "false", Description: "Reject players whose network differs from the login network."},
		{Name: "pvp", Type: "boolean", Default: "true", Description: "Allow player-versus-player combat. Now a game rule.", Until: "1.21.9"},
		{Name: "query.port", Type: "port", Default: "25565", Description: "UDP port for the query listener."},
		{Name: "rate-limit", Type: "integer", Default: "0", Description: "Maximum packets per second before a kick; 0 disables.", Min: new(int64(0)), Allowed: "0 or more", Since: "1.16.2"},
		{Name: "rcon.password", Type: "string", Default: "", Description: "RCON password; required when RCON is enabled."},
		{Name: "rcon.port", Type: "port", Default: "25575", Description: "TCP port for RCON."},
		{Name: "region-file-compression", Type: "enum", Default: "deflate", Description: "Compression for newly saved region files.", Allowed: "deflate, lz4, none", Values: []string{"deflate", "lz4", "none"}, Since: "1.20.5"},
		{Name: "require-resource-pack", Type: "boolean", Default: "false", Description: "Disconnect players who decline the resource pack.", Since: "1.17"},
		{Name: "resource-pack", Type: "string", Default: "", Description: "URL of the resource pack offered on join."},
		{Name: "resource-pack-id", Type: "uuid", Default: "", Description: "Optional UUID identifying the resource pack.", Since: "1.20.3"},
		{Name: "resource-pack-prompt", Type: "string", Default: "", Description: "Custom resource-pack prompt text.", Since: "1.17"},
		{Name: "resource-pack-sha1", Type: "sha1", Default: "", Description: "Optional SHA-1 digest of the resource pack."},
		{Name: "server-ip", Type: "ip", Default: "", Description: "IP address to bind; empty binds all interfaces."},
		{Name: "server-port", Type: "port", Default: "25565", Description: "TCP port players connect to."},
		{Name: "simulation-distance", Type: "integer", Default: "10", Description: "Chunk radius in which entities tick.", Min: new(int64(2)), Max: new(int64(32)), Allowed: "2-32", Since: "1.18"},
		{Name: "spawn-animals", Type: "boolean", Default: "true", Description: "Allow animals to spawn. Removed.", Until: "1.21.2"},
		{Name: "spawn-monsters", Type: "boolean", Default: "true", Description: "Allow monsters to spawn. Now a game rule.", Until: "1.21.9"},
		{Name: "spawn-npcs", Type: "boolean", Default: "true", Description: "Allow villagers and other NPCs to spawn. Removed.", Until: "1.21.2"},
		{Name: "spawn-protection", Type: "integer", Default: "16", Description: "Spawn protection radius; 0 disables it.", Min: new(int64(0)), Allowed: "0 or more"},
		{Name: "status-heartbeat-interval", Type: "integer", Default: "0", Description: "Seconds between management-server heartbeat notifications.", Min: new(int64(0)), Allowed: "0 or more", Since: "1.21.9"},
		{Name: "sync-chunk-writes", Type: "boolean", Default: "true", Description: "Wait for synchronous chunk writes to disk."},
		{Name: "text-filtering-config", Type: "string", Default: "", Description: "Settings for an external chat-filter service.", Since: "1.16.4"},
		{Name: "text-filtering-version", Type: "enum", Default: "0", Description: "Version of the external chat-filter format.", Allowed: "0 or 1", Values: []string{"0", "1"}, Since: "1.21.2"},
		{Name: "use-native-transport", Type: "boolean", Default: "true", Description: "Use the platform's optimized network transport."},
		{Name: "view-distance", Type: "integer", Default: "10", Description: "Chunk radius sent to clients.", Min: new(int64(2)), Max: new(int64(32)), Allowed: "2-32"},
		{Name: "white-list", Type: "boolean", Default: "false", Description: "Only allow players in whitelist.json to join."},
		{Name: "max-build-height", Type: "integer", Default: "256", Description: "Maximum build height. Removed.", Until: "1.17"},
		{Name: "snooper-enabled", Type: "boolean", Default: "false", Description: "Send anonymous usage data to Mojang. Removed.", Until: "1.18"},
		{Name: "previews-chat", Type: "boolean", Default: "false", Description: "Preview chat messages. Removed.", Since: "1.19", Until: "1.19.3"},
		{Name: "chat-spam-threshold-seconds", Type: "integer", Default: "10", Description: "Chat spam kick threshold; 0 disables.", Min: new(int64(0)), Allowed: "0 or more", Since: "26.2"},
		{Name: "command-spam-threshold-seconds", Type: "integer", Default: "10", Description: "Command spam kick threshold; 0 disables.", Min: new(int64(0)), Allowed: "0 or more", Since: "26.2"},
		{Name: "allowed-connection-ids", Type: "string", Default: "", Description: "Comma-separated connection ids allowed to join.", Since: "26.4"},
		{Name: "enable-legacy-status", Type: "boolean", Default: "true", Description: "Answer status requests from pre-1.7 clients.", Since: "26.4"},
		{Name: "key-rotation-period-seconds", Type: "integer", Default: "60", Description: "Minimum seconds between server key rotations.", Min: new(int64(0)), Allowed: "0 or more", Since: "26.4"},
		{Name: "status-contact-details", Type: "string", Default: "", Description: "Owner contact information for status responses.", Since: "26.4"},
	}
}
