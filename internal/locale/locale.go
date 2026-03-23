package locale

// Key is a translation key.
type Key string

// current holds the active language map.
var current map[Key]string

// currentLang holds the active language code.
var currentLang string

// SetLanguage switches the active locale. Falls back to English.
func SetLanguage(lang string) {
	switch lang {
	case "zh-TW", "zh-tw", "zh":
		current = zhTW
		currentLang = "zh-TW"
	default:
		current = en
		currentLang = "en"
	}
}

// T returns the translated string for the given key. Falls back to key name if missing.
func T(k Key) string {
	if current == nil {
		current = en
	}
	if s, ok := current[k]; ok {
		return s
	}
	return string(k)
}

// ResponseInstruction returns the agent response language instruction.
// Returns empty string for English (Claude's default).
func ResponseInstruction() string {
	switch currentLang {
	case "zh-TW":
		return "Always respond in Traditional Chinese (zh-TW).\n\n"
	default:
		return ""
	}
}
