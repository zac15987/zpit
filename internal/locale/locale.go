package locale

// Key is a translation key.
type Key string

// current holds the active language map.
var current map[Key]string

// SetLanguage switches the active locale. Falls back to English.
func SetLanguage(lang string) {
	switch lang {
	case "zh-TW", "zh-tw", "zh":
		current = zhTW
	default:
		current = en
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
