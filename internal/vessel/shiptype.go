package vessel

func category(code int) string {
	switch {
	case code == 30:
		return "Fishing"
	case code == 31 || code == 32:
		return "Towing"
	case code == 36:
		return "Sailing"
	case code == 37:
		return "Pleasure"
	case code >= 40 && code <= 49:
		return "High-speed"
	case code == 50:
		return "Pilot"
	case code == 51:
		return "SAR"
	case code == 52:
		return "Tug"
	case code >= 60 && code <= 69:
		return "Passenger"
	case code >= 70 && code <= 79:
		return "Cargo"
	case code >= 80 && code <= 89:
		return "Tanker"
	default:
		return "Other"
	}
}

// TypeLabel is the human label for an AIS ship type code.
func TypeLabel(code int) string { return category(code) }

// TypeIcon names the fallback icon (static/icons/<name>.svg).
func TypeIcon(code int) string {
	switch category(code) {
	case "Fishing":
		return "fishing"
	case "Sailing", "Pleasure":
		return "sail"
	default:
		return "ship"
	}
}
