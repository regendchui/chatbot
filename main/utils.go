package main // Use main package so utility helpers are available across files.

import ( // String and Unicode helpers.
	"strings" // Split/clean JID values.
	"unicode" // Classify digits for phone normalization.
) // End import.

func extractPhone(jid string) string { // Extract phone part from JID such as 628xx@s.whatsapp.net.
	trimmed := strings.TrimSpace(jid) // Remove surrounding spaces from input value.
	if trimmed == "" {                // Handle empty input safely.
		return "" // Return empty phone when input is empty.
	}

	parts := strings.Split(trimmed, "@")                     // Split JID into phone and domain parts.
	if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" { // Check if phone part exists.
		local := strings.TrimSpace(parts[0]) // Read JID local-part (before @).
		// Device-specific IDs can look like "<phone>:<device>", e.g. 85295934925:5.
		// Keep only the base phone and drop the per-device suffix.
		if i := strings.Index(local, ":"); i >= 0 {
			local = strings.TrimSpace(local[:i])
		}
		digits := digitsOnly(local)
		if digits != "" {
			return digits // Return strictly normalized phone digits.
		}
		return local // Fallback for uncommon non-digit IDs.
	}

	return trimmed // Fallback to original trimmed value when split is unexpected.
} // End extractPhone function.

// digitsOnly keeps Unicode decimal digits (for international phone strings).
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
