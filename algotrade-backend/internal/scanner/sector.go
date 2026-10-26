package scanner

import (
	"database/sql"
	"strings"
)

// Sector ID enumeration (1 to 12)
const (
	SectorNifty50    uint8 = 1
	SectorAuto       uint8 = 2
	SectorBank       uint8 = 3
	SectorFinService uint8 = 4
	SectorFMCG       uint8 = 5
	SectorIT         uint8 = 6
	SectorMedia      uint8 = 7
	SectorMetal      uint8 = 8
	SectorPharma     uint8 = 9
	SectorRealty     uint8 = 10
	SectorEnergy     uint8 = 11
	SectorInfra      uint8 = 12
)

// SectorInfo holds metadata about a sector index
type SectorInfo struct {
	ID    uint8
	Name  string
	Token uint32
}

var (
	// SectorRegistry maps sector ID to its metadata and official Kite index token
	SectorRegistry = map[uint8]SectorInfo{
		SectorNifty50:    {ID: SectorNifty50, Name: "NIFTY 50", Token: 256265},
		SectorAuto:       {ID: SectorAuto, Name: "NIFTY AUTO", Token: 263433},
		SectorBank:       {ID: SectorBank, Name: "NIFTY BANK", Token: 260105},
		SectorFinService: {ID: SectorFinService, Name: "NIFTY FIN SERVICE", Token: 257801},
		SectorFMCG:       {ID: SectorFMCG, Name: "NIFTY FMCG", Token: 261897}, // verified from Kite instruments master
		SectorIT:         {ID: SectorIT, Name: "NIFTY IT", Token: 259849},
		SectorMedia:      {ID: SectorMedia, Name: "NIFTY MEDIA", Token: 263945},
		SectorMetal:      {ID: SectorMetal, Name: "NIFTY METAL", Token: 263689},
		SectorPharma:     {ID: SectorPharma, Name: "NIFTY PHARMA", Token: 262409}, // verified from Kite instruments master
		SectorRealty:     {ID: SectorRealty, Name: "NIFTY REALTY", Token: 261129}, // verified from Kite instruments master
		SectorEnergy:     {ID: SectorEnergy, Name: "NIFTY ENERGY", Token: 261641}, // verified from Kite instruments master
		SectorInfra:      {ID: SectorInfra, Name: "NIFTY INFRA", Token: 261385},  // verified from Kite instruments master
	}

	// BuiltInConstituents maps symbols to Sector IDs as Layer 2 resolution
	BuiltInConstituents = map[string]uint8{
		// AUTO
		"BAJAJ-AUTO": SectorAuto, "BHARATFORG": SectorAuto, "BOSCHLTD": SectorAuto,
		"EICHERMOT": SectorAuto, "HEROMOTOCO": SectorAuto, "M&M": SectorAuto,
		"MARUTI": SectorAuto, "MRF": SectorAuto, "TATAMOTORS": SectorAuto,
		"TVSMOTOR": SectorAuto, "ASHOKLEY": SectorAuto, "BALKRISIND": SectorAuto,
		"EXIDEIND": SectorAuto, "MOTHERSON": SectorAuto, "APOLLOTYRE": SectorAuto,

		// BANK
		"AUBANK": SectorBank, "AXISBANK": SectorBank, "BANDHANBNK": SectorBank,
		"BANKBARODA": SectorBank, "CANBK": SectorBank, "FEDERALBNK": SectorBank,
		"HDFCBANK": SectorBank, "ICICIBANK": SectorBank, "IDFCFIRSTB": SectorBank,
		"INDUSINDBK": SectorBank, "KOTAKBANK": SectorBank, "PNB": SectorBank,
		"SBIN": SectorBank,

		// FINANCIAL SERVICES
		"BAJFINANCE": SectorFinService, "BAJAJFINSV": SectorFinService, "CHOLAFIN": SectorFinService,
		"HDFCLIFE": SectorFinService, "ICICIGI": SectorFinService, "ICICIPRULI": SectorFinService,
		"MUTHOOTFIN": SectorFinService, "PFC": SectorFinService, "RECLTD": SectorFinService,
		"SBICARD": SectorFinService, "SBILIFE": SectorFinService, "SHRIRAMFIN": SectorFinService,
		"LICHSGFIN": SectorFinService, "MANAPPURAM": SectorFinService, "M&MFIN": SectorFinService,

		// FMCG
		"BRITANNIA": SectorFMCG, "COLPAL": SectorFMCG, "DABUR": SectorFMCG,
		"GODREJCP": SectorFMCG, "HINDUNILVR": SectorFMCG, "ITC": SectorFMCG,
		"MARICO": SectorFMCG, "NESTLEIND": SectorFMCG, "TATACONSUM": SectorFMCG,
		"UBL": SectorFMCG, "MCDOWELL-N": SectorFMCG, "RADICO": SectorFMCG,

		// IT
		"COFORGE": SectorIT, "HCLTECH": SectorIT, "INFY": SectorIT,
		"LTIM": SectorIT, "LTTS": SectorIT, "MPHASIS": SectorIT,
		"OFSS": SectorIT, "PERSISTENT": SectorIT, "TCS": SectorIT,
		"TECHM": SectorIT, "WIPRO": SectorIT,

		// MEDIA
		"SUNTV": SectorMedia, "PVRINOX": SectorMedia, "ZEEL": SectorMedia,
		"NAZARA": SectorMedia, "NETWORK18": SectorMedia,

		// METAL
		"APLAPOLLO": SectorMetal, "HINDALCO": SectorMetal, "HINDCOPPER": SectorMetal,
		"HINDZINC": SectorMetal, "JINDALSTEL": SectorMetal, "JSL": SectorMetal,
		"JSWSTEEL": SectorMetal, "NATIONALUM": SectorMetal, "NMDC": SectorMetal,
		"SAIL": SectorMetal, "TATASTEEL": SectorMetal, "VEDL": SectorMetal,

		// PHARMA & HEALTHCARE
		"ABBOTINDIA": SectorPharma, "ALKEM": SectorPharma, "AUROPHARMA": SectorPharma,
		"BIOCON": SectorPharma, "CIPLA": SectorPharma, "DIVISLAB": SectorPharma,
		"DRREDDY": SectorPharma, "GLENMARK": SectorPharma, "IPCALAB": SectorPharma,
		"LUPIN": SectorPharma, "MANKIND": SectorPharma, "SUNPHARMA": SectorPharma,
		"TORNTPHARM": SectorPharma, "ZYDUSLIFE": SectorPharma, "APOLLOHOSP": SectorPharma,
		"MAXHEALTH": SectorPharma, "FORTIS": SectorPharma,

		// REALTY
		"BRIGADE": SectorRealty, "DLF": SectorRealty, "GODREJPROP": SectorRealty,
		"LODHA": SectorRealty, "OBEROIRLTY": SectorRealty, "PHOENIXLTD": SectorRealty,
		"PRESTIGE": SectorRealty, "SOBHA": SectorRealty,

		// ENERGY (OIL, GAS & POWER)
		"BPCL": SectorEnergy, "GAIL": SectorEnergy, "IOC": SectorEnergy,
		"ONGC": SectorEnergy, "RELIANCE": SectorEnergy, "NTPC": SectorEnergy,
		"POWERGRID": SectorEnergy, "TATAPOWER": SectorEnergy, "ADANIGREEN": SectorEnergy,
		"ADANIPOWER": SectorEnergy, "COALINDIA": SectorEnergy, "PETRONET": SectorEnergy,
		"IGL": SectorEnergy, "MGL": SectorEnergy, "GUJGASLTD": SectorEnergy,

		// INFRA & CAPITAL GOODS
		"ADANIPORTS": SectorInfra, "BHARTIARTL": SectorInfra, "INDIGO": SectorInfra,
		"LT": SectorInfra, "SIEMENS": SectorInfra, "ABB": SectorInfra,
		"BEL": SectorInfra, "HAL": SectorInfra, "BHEL": SectorInfra,
		"CUMMINSIND": SectorInfra, "GMRINFRA": SectorInfra, "IRCTC": SectorInfra,
		"CONCOR": SectorInfra, "GRASIM": SectorInfra, "ULTRACEMCO": SectorInfra,
		"AMBUJACEM": SectorInfra, "SHREECEM": SectorInfra, "ACC": SectorInfra,
	}
)

// ResolveSector determines the sector ID, Name, and Benchmark Index Token for a stock symbol.
// Layer 1: Query SQLite sector_metadata table.
// Layer 2: Built-in sector mapping for active F&O equities.
// Layer 3: Fallback to NIFTY 50 (Token 256265).
func ResolveSector(db *sql.DB, symbol string) (uint8, string, uint32) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))

	// Layer 1: SQLite sector_metadata lookup if db is available
	if db != nil {
		var secName string
		var secToken int
		err := db.QueryRow("SELECT sector_name, sector_token FROM sector_metadata WHERE tradingsymbol = ?", sym).Scan(&secName, &secToken)
		if err == nil && secName != "" {
			for id, info := range SectorRegistry {
				if strings.EqualFold(info.Name, secName) {
					token := info.Token
					if secToken > 0 {
						token = uint32(secToken)
					}
					return id, info.Name, token
				}
			}
		}
	}

	// Layer 2: BuiltInConstituents
	if secID, ok := BuiltInConstituents[sym]; ok {
		if info, found := SectorRegistry[secID]; found {
			return info.ID, info.Name, info.Token
		}
	}

	// Layer 3: NIFTY 50 fallback
	nifty := SectorRegistry[SectorNifty50]
	return nifty.ID, nifty.Name, nifty.Token
}

// GetSectorName returns human-readable sector name for a given sector ID
func GetSectorName(id uint8) string {
	if info, ok := SectorRegistry[id]; ok {
		return info.Name
	}
	return "NIFTY 50"
}
