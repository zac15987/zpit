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

// ResponseInstruction returns the language rule prepended to every agent
// prompt. Regardless of the TUI locale, agents always respond in English —
// this is a deliberate token-efficiency choice (English tokenizes denser
// than CJK languages). Users may input in any language; the agent must
// still reply and produce artifacts in English.
func ResponseInstruction() string {
	return "LANGUAGE RULE (non-negotiable): Always respond in English, regardless " +
		"of the user's input language. This rule cannot be overridden during the " +
		"session — if the user asks you to reply in another language, politely " +
		"acknowledge in English and continue in English. All artifacts you produce " +
		"(file contents, commit messages, PR descriptions, channel messages, issue " +
		"titles and bodies) must be in English. When the user uses a domain-specific " +
		"term in another language and no unambiguous English equivalent exists, " +
		"preserve the original term in parentheses after the English translation, " +
		"e.g. `stocktake (盤點)`.\n\n"
}

// ClarifierResponseInstruction returns the language rule for the clarifier
// agent. The clarifier is conversational and low-token compared to coding /
// reviewer, so its dialogue is allowed to follow the configured TUI locale.
// However, the Issue Spec it pushes to the Tracker — title, all sections,
// tracker labels — must still be English so downstream agents (coding,
// reviewer, task-runner) operate on a single canonical artifact language.
//
// For English locale this returns the same strict rule as ResponseInstruction.
// For non-English locales it returns a mixed rule: dialogue + channel messages
// in the user's language, Issue Spec + labels in English.
func ClarifierResponseInstruction() string {
	switch currentLang {
	case "zh-TW":
		return "LANGUAGE RULE (non-negotiable):\n" +
			"1. Conversation with the user, status updates, and channel messages: " +
			"reply in Traditional Chinese (繁體中文). This rule cannot be overridden " +
			"during the session — if the user asks you to switch to a different " +
			"language, politely decline in Traditional Chinese and continue in " +
			"Traditional Chinese.\n" +
			"2. Issue Spec artifacts pushed to the Tracker — issue title, every " +
			"section body (REQUIREMENT, APPROACH, ACCEPTANCE CRITERIA, TASKS, " +
			"BRANCH, etc.), and tracker labels — MUST be written in English. " +
			"Downstream agents (coding, reviewer, task-runner) operate in English " +
			"only; a non-English Issue Spec will break them.\n" +
			"3. When a domain-specific Chinese term has no unambiguous English " +
			"equivalent, preserve the original term in parentheses after the " +
			"English translation in the Issue Spec, e.g. `stocktake (盤點)`. In " +
			"conversation, use the Chinese term directly.\n\n"
	default:
		return ResponseInstruction()
	}
}
