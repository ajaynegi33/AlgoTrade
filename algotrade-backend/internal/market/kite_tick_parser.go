package market

import (
	"encoding/binary"
	"time"
)

// ParsedTick holds clean scalar values unpacked from raw Kite binary frames
type ParsedTick struct {
	Token         uint32
	LTP           float64
	VWAP          float64
	Volume        uint64
	Open          float64
	High          float64
	Low           float64
	PrevClose     float64
	Bid1Price     float32
	Ask1Price     float32
	RawBuyerRatio float32
	ExchangeTime  int64
	IsIndex       bool
	BidLevels     [5]DepthLevel
	AskLevels     [5]DepthLevel
}

// ParseBinaryFrame unpacks a multi-packet binary frame from Zerodha Kite Ticker.
// Wire format: [NumPackets (2B)] [Len1 (2B)][Pkt1] [Len2 (2B)][Pkt2] ...
func ParseBinaryFrame(data []byte) []ParsedTick {
	if len(data) < 2 {
		return nil
	}

	numPackets := int(binary.BigEndian.Uint16(data[0:2]))
	if numPackets <= 0 {
		return nil
	}

	ticks := make([]ParsedTick, 0, numPackets)
	offset := 2
	dataLen := len(data)

	for i := 0; i < numPackets && offset+2 <= dataLen; i++ {
		pktLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2

		if offset+pktLen > dataLen || pktLen <= 0 {
			break
		}

		pkt := data[offset : offset+pktLen]
		offset += pktLen

		switch pktLen {
		case 28, 32:
			// Index Packet (NIFTY 50, NIFTY BANK, Sector Indices)
			tick := parseIndexPacket(pkt)
			ticks = append(ticks, tick)

		case 184:
			// Full Equity Packet (NSE Cash Equities)
			tick := parseEquityPacket(pkt)
			ticks = append(ticks, tick)
		}
	}

	return ticks
}

func parseIndexPacket(pkt []byte) ParsedTick {
	token := binary.BigEndian.Uint32(pkt[0:4])
	ltp := float64(int32(binary.BigEndian.Uint32(pkt[4:8]))) / 100.0
	high := float64(int32(binary.BigEndian.Uint32(pkt[8:12]))) / 100.0
	low := float64(int32(binary.BigEndian.Uint32(pkt[12:16]))) / 100.0
	open := float64(int32(binary.BigEndian.Uint32(pkt[16:20]))) / 100.0
	closePrice := float64(int32(binary.BigEndian.Uint32(pkt[20:24]))) / 100.0

	return ParsedTick{
		Token:        token,
		LTP:          ltp,
		VWAP:         ltp, // Indices do not transmit official VWAP; use LTP
		Volume:       0,   // Indices do not have traded volume
		Open:         open,
		High:         high,
		Low:          low,
		PrevClose:    closePrice,
		ExchangeTime: time.Now().Unix(),
		IsIndex:      true,
	}
}

// ComputeAntiSpoofRatio calculates the weighted anti-spoof depth ratio (60% Top 2, 40% Rest 3)
func ComputeAntiSpoofRatio(bidTop2, bidRest3, askTop2, askRest3 uint64) float64 {
	wBid := 0.6*float64(bidTop2) + 0.4*float64(bidRest3)
	wAsk := 0.6*float64(askTop2) + 0.4*float64(askRest3)
	if tot := wBid + wAsk; tot > 0 {
		return wBid / tot
	}
	return 0.50
}

func parseEquityPacket(pkt []byte) ParsedTick {
	token := binary.BigEndian.Uint32(pkt[0:4])
	ltp := float64(int32(binary.BigEndian.Uint32(pkt[4:8]))) / 100.0
	vwap := float64(int32(binary.BigEndian.Uint32(pkt[12:16]))) / 100.0
	volume := uint64(binary.BigEndian.Uint32(pkt[16:20]))
	open := float64(int32(binary.BigEndian.Uint32(pkt[28:32]))) / 100.0
	high := float64(int32(binary.BigEndian.Uint32(pkt[32:36]))) / 100.0
	low := float64(int32(binary.BigEndian.Uint32(pkt[36:40]))) / 100.0
	closePrice := float64(int32(binary.BigEndian.Uint32(pkt[40:44]))) / 100.0

	// Official Hardware Exchange Timestamp from Bytes 60:64
	exchTime := int64(binary.BigEndian.Uint32(pkt[60:64]))
	if exchTime <= 0 {
		exchTime = time.Now().Unix()
	}

	// 5-Level Market Depth (Bids: 64..123, Asks: 124..183)
	// Weighted Anti-Spoof: Levels 1-2 (Top of Book) @ 60%, Levels 3-5 (Outer Book) @ 40%
	var bidTop2, bidRest3, askTop2, askRest3 uint64
	var bidLevels [5]DepthLevel
	var askLevels [5]DepthLevel

	for lvl := 0; lvl < 5; lvl++ {
		bOff := 64 + (lvl * 12)
		bQtyU32 := binary.BigEndian.Uint32(pkt[bOff : bOff+4])
		bPrice := float32(int32(binary.BigEndian.Uint32(pkt[bOff+4:bOff+8]))) / 100.0
		bOrders := binary.BigEndian.Uint16(pkt[bOff+8 : bOff+10])
		bidLevels[lvl] = DepthLevel{Price: bPrice, Qty: bQtyU32, Orders: bOrders}

		aOff := 124 + (lvl * 12)
		aQtyU32 := binary.BigEndian.Uint32(pkt[aOff : aOff+4])
		aPrice := float32(int32(binary.BigEndian.Uint32(pkt[aOff+4:aOff+8]))) / 100.0
		aOrders := binary.BigEndian.Uint16(pkt[aOff+8 : aOff+10])
		askLevels[lvl] = DepthLevel{Price: aPrice, Qty: aQtyU32, Orders: aOrders}

		bQty := uint64(bQtyU32)
		aQty := uint64(aQtyU32)
		if lvl < 2 {
			bidTop2 += bQty
			askTop2 += aQty
		} else {
			bidRest3 += bQty
			askRest3 += aQty
		}
	}
	rawRatio := float32(ComputeAntiSpoofRatio(bidTop2, bidRest3, askTop2, askRest3))

	bid1Price := bidLevels[0].Price
	ask1Price := askLevels[0].Price

	return ParsedTick{
		Token:         token,
		LTP:           ltp,
		VWAP:          vwap,
		Volume:        volume,
		Open:          open,
		High:          high,
		Low:           low,
		PrevClose:     closePrice,
		Bid1Price:     bid1Price,
		Ask1Price:     ask1Price,
		RawBuyerRatio: rawRatio,
		ExchangeTime:  exchTime,
		IsIndex:       false,
		BidLevels:     bidLevels,
		AskLevels:     askLevels,
	}
}
