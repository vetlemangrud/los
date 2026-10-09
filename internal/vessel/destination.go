package vessel

import (
	"regexp"
	"strings"
	"unicode"
)

// locodeRe matches UN/LOCODE-shaped text: country + optional separator + 3-char place.
var locodeRe = regexp.MustCompile(`^([A-Z]{2})([ \-_]?)([A-Z2-9]{3})$`)

// ports maps UN/LOCODEs of common North Sea, Skagerrak and Baltic ports to
// a display name. Unknown codes fall back to the raw text.
var ports = map[string]string{
	// Norway
	"NOOSL": "Oslo", "NOBGO": "Bergen", "NOSVG": "Stavanger", "NOKRS": "Kristiansand",
	"NOTRD": "Trondheim", "NOAES": "Ålesund", "NOHAU": "Haugesund", "NOTOS": "Tromsø",
	"NOBOO": "Bodø", "NOFRK": "Fredrikstad", "NOLAR": "Larvik", "NOARE": "Arendal",
	"NOPOR": "Porsgrunn", "NOBRE": "Brevik", "NOMSS": "Moss", "NODRM": "Drammen",
	"NOHFT": "Hammerfest", "NOKSU": "Kristiansund", "NOMOL": "Molde", "NONVK": "Narvik",
	"NOFRO": "Florø", "NOMON": "Mongstad", "NOEGE": "Egersund", "NOFAN": "Farsund",
	// Sweden
	"SEGOT": "Gothenburg", "SESTO": "Stockholm", "SEMMA": "Malmö", "SEHEL": "Helsingborg",
	"SELYS": "Lysekil",
	// Denmark
	"DKCPH": "Copenhagen", "DKAAR": "Aarhus", "DKFRC": "Fredericia", "DKEBJ": "Esbjerg",
	"DKSKA": "Skagen", "DKHIR": "Hirtshals", "DKFDH": "Frederikshavn", "DKAAL": "Aalborg",
	// Germany
	"DEHAM": "Hamburg", "DEBRV": "Bremerhaven", "DEBRE": "Bremen", "DEWVN": "Wilhelmshaven",
	"DEKEL": "Kiel", "DECUX": "Cuxhaven", "DEEME": "Emden", "DEROS": "Rostock",
	// Netherlands, Belgium, France
	"NLRTM": "Rotterdam", "NLAMS": "Amsterdam", "NLVLI": "Vlissingen", "NLEEM": "Eemshaven",
	"NLDZL": "Delfzijl", "NLIJM": "IJmuiden", "BEANR": "Antwerp", "BEZEE": "Zeebrugge",
	"BEGNE": "Ghent", "FRLEH": "Le Havre", "FRDKK": "Dunkirk",
	// United Kingdom
	"GBABD": "Aberdeen", "GBGRG": "Grangemouth", "GBIMM": "Immingham", "GBFXT": "Felixstowe",
	"GBTEE": "Teesport", "GBHUL": "Hull", "GBSOU": "Southampton", "GBLON": "London",
	"GBLER": "Lerwick", "GBPHD": "Peterhead",
	// Baltic and North Atlantic
	"PLGDN": "Gdańsk", "PLGDY": "Gdynia", "PLSZZ": "Szczecin", "FIHEL": "Helsinki",
	"EETLL": "Tallinn", "LVRIX": "Riga", "LTKLJ": "Klaipėda", "RULED": "St. Petersburg",
	"ISREY": "Reykjavík", "FOTHO": "Tórshavn",
}

// ParseDestination turns a raw AIS destination into display text.
func ParseDestination(raw string) string {
	s := strings.ToUpper(strings.Trim(raw, "@ \t"))
	// "FROM>TO" is a common convention; keep the destination part.
	if i := strings.LastIndex(s, ">"); i >= 0 {
		s = strings.Trim(s[i+1:], "@ \t")
	}
	if s == "" {
		return "Destination unknown"
	}
	if m := locodeRe.FindStringSubmatch(s); m != nil {
		if name, ok := ports[m[1]+m[3]]; ok {
			return name + ", " + m[1]
		}
		if m[2] != "" { // clearly a code ("NO XYZ"), just not one we know
			return m[1] + " " + m[3]
		}
	}
	return titleCase(s)
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
